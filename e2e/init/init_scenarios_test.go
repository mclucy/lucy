//go:build e2e

package init

const (
	cobblemon1211        = "cobblemon-1211"
	fabricExecutable262  = "fabric-executable-262"
	fabricExecutable1214 = "fabric-executable-1214"
	vanilla262           = "vanilla-262"
	arclightFabric       = "arclight-fabric"

	cobblemon1211Launcher  = "fabric-server-mc.1.21.1-loader.0.19.5-launcher.1.1.2.jar"
	fabric262Launcher      = "fabric-server-mc.26.2-loader.0.19.3-launcher.1.1.2.jar"
	fabric1214Launcher     = "fabric-server-mc.1.21.4-loader.0.16.9-launcher.1.0.1.jar"
	fabricShim             = "fabric-server-launch.jar"
	arclightFabricLauncher = "arclight-fabric-1.21.1-1.0.1-8ec9529.jar"
	paperJar               = "paper-1.21.11-130.jar"
	forge1122Jar           = "forge-1.12.2-14.23.5.2860-universal.jar"

	forge1201EntryPoint   = "libraries/net/minecraftforge/forge/1.20.1-47.4.10/unix_args.txt"
	forge12111EntryPoint  = "libraries/net/minecraftforge/forge/1.21.11-61.2.0/forge-1.21.11-61.2.0-shim.jar"
	neoforge211EntryPoint = "libraries/net/neoforged/neoforge/21.1.248/unix_args.txt"
	neoforge262EntryPoint = "libraries/net/neoforged/neoforge/26.2.0.67/unix_args.txt"
)

type manifestDocument struct {
	Server struct {
		Minecraft string `yaml:"minecraft"`
		Core      struct {
			Product        string `yaml:"product"`
			ProductVersion string `yaml:"product_version"`
		} `yaml:"core"`
		Packages map[string]map[string]string `yaml:"packages"`
	} `yaml:"server"`
}

// initializeScenario describes one real directory composition and the
// assertions TestInitialize makes about the manifest and lock that init
// writes for it.
type initializeScenario struct {
	// sandbox is the generated sandbox to clone; the case id when empty.
	sandbox string
	// prune removes installation output that a reused sandbox may carry, so
	// the case starts from the files the fixture ships.
	prune []string
	// prepare runs Lucy commands that build the pre-init directory, such as a
	// core-only installation.
	prepare []initializeCommand
	// artifacts copy pinned files from generated sandboxes into the case.
	artifacts []initializeArtifact
	// symlinks create real filesystem aliases inside the case.
	symlinks []initializeSymlink
	// removeBefore removes generated state after preparation and before init.
	removeBefore []string
	// modifyFiles appends a ZIP comment to these installed artifacts after the
	// receipt is generated, so their bytes no longer match the recorded digest.
	modifyFiles []string
	// force passes --force to the initialization under test.
	force bool
	// selectEntryPoint passes --entry-point for this workspace-relative file.
	selectEntryPoint string
	// expectFailure requires init to refuse the directory.
	expectFailure bool
	// guidance lists entry points whose exact selection command a refusal must
	// print.
	guidance []string
	// informational lists entry points that init and live status must report
	// at INFO severity, naming the path.
	informational []string
	// wantReceipt keeps the installation receipt assertion; init itself never
	// stages one.
	wantReceipt bool
	// status asserts the live server identity after init, including after a
	// refusal that leaves the declared layout unresolved for observation.
	status *statusExpectation
	// unchanged lists files whose bytes must survive preparation and init.
	unchanged []string

	minecraft   string
	product     string
	coreVersion string
	minPackages int
	// packages maps a loader to the references that the initializer must write.
	packages map[string]map[string]string
	// absent lists references that the initializer must omit because their jars
	// are client-only.
	absent map[string][]string
}

// initializeCommand is one Lucy invocation that builds the pre-init directory.
// selectEntryPoint is an ephemeral choice for this invocation only; it is
// never persisted.
type initializeCommand struct {
	args             []string
	selectEntryPoint string
}

// initializeArtifact copies one pinned file from a generated sandbox.
type initializeArtifact struct {
	// sandbox is the fixture that holds the file; the case sandbox when empty.
	sandbox string
	source  string
	dest    string
}

// initializeSymlink creates a real filesystem alias inside the case.
type initializeSymlink struct {
	target string
	link   string
}

// statusExpectation describes the server identity `status --json-compact`
// reports. components maps "ecosystem:name" to the expected version.
type statusExpectation struct {
	ecosystem  string
	name       string
	version    string
	components map[string]string
	// path is the workspace-relative entry point live status must report. It
	// names the file status observes now, not a path persisted by init.
	path string
	// missing requires live status to report no server and warn instead of
	// adopting a runtime that does not match the declaration.
	missing bool
	// ambiguous lists competing workspace-relative entry points. It requires
	// live status to report no server and an ERROR finding naming each one; a
	// declaration with several compatible installations never auto-selects.
	ambiguous []string
	// warning lists the automatically selected entry point followed by the
	// competing entry points that a WARN-level finding must name.
	warning []string
}

// installationOutputPaths are the files the installer owns for a Fabric
// runtime. They are not part of a fixture and must not leak into an
// initialization case that starts from the files the modpack ships.
var installationOutputPaths = []string{
	".lucy/install.yaml",
	".lucy/writer.lock",
	"lucy.yaml",
	"lucy-lock.yaml",
	"fabric-server-launch.jar",
	"fabric-server-launcher.properties",
	"libraries",
	"server.jar",
	"run.sh",
	"run.bat",
	"versions",
	"cache",
	".fabric",
}

// runtimeInstallPreparation declares a core-only runtime and installs it, so
// the case exercises the launcher seam without adopting the fixture's mods.
func runtimeInstallPreparation(minecraft, core string) []initializeCommand {
	return []initializeCommand{
		{args: []string{
			"create", "--minecraft", minecraft, "--core", core, "--no-interactive",
		}},
		{args: []string{"install", "--locked", "--overwrite", "--no-interactive"}},
	}
}

var initializeScenarios = map[string]initializeScenario{
	"cobblemon-1211": {
		minecraft: "1.21.1", product: "fabric", coreVersion: "0.19.5",
		minPackages: 30,
		packages: map[string]map[string]string{
			"fabric": {
				"modrinth:advanced-loot-info": "1.21.1-2.4.0",
				"modrinth:cobblemon":          "1.8.2",
				"modrinth:fabric-api":         "0.116.17+1.21.1",
			},
		},
		absent: map[string][]string{
			"fabric": {"modrinth:enchdesc", "modrinth:tipsmod"},
		},
	},
	"create-plus-1211": {
		minecraft: "1.21.1", product: "neoforge", coreVersion: "21.1.233",
		minPackages: 100,
		packages: map[string]map[string]string{
			"neoforge": {
				"modrinth:aileron":          "1.21.1-neoforge-1.1.4",
				"modrinth:kotlin-for-forge": "5.11.0",
				"modrinth:txnilib":          "neoforge-1.21.1-1.0.24",
			},
		},
	},

	// A real Fabric meta launcher from the Cobblemon pack and the installer's
	// shim are two entry points of one runtime. Init must keep the receipt,
	// select the managed shim, and leave the pack launcher untouched.
	"fabric-in-place-composition": {
		sandbox:      cobblemon1211,
		prune:        installationOutputPaths,
		prepare:      runtimeInstallPreparation("1.21.1", "fabric@0.19.5"),
		removeBefore: []string{"lucy.yaml"},
		minecraft:    "1.21.1", product: "fabric", coreVersion: "0.19.5",
		minPackages: 30,
		packages: map[string]map[string]string{
			"fabric": {
				"modrinth:advanced-loot-info": "1.21.1-2.4.0",
				"modrinth:cobblemon":          "1.8.2",
				"modrinth:fabric-api":         "0.116.17+1.21.1",
			},
		},
		absent: map[string][]string{
			"fabric": {"modrinth:enchdesc", "modrinth:tipsmod"},
		},
		wantReceipt:   true,
		unchanged:     []string{cobblemon1211Launcher},
		informational: []string{cobblemon1211Launcher},
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.5",
			path: fabricShim,
			components: map[string]string{
				"minecraft:minecraft":  "1.21.1",
				"fabric:fabric-loader": "0.19.5",
			},
		},
	},

	// Installation replaces the declared meta launcher with its managed shim.
	// The selected path is ephemeral: when the pack launcher disappears, the
	// declared installation identity still matches the shim.
	"fabric-in-place-managed-replacement": {
		sandbox: fabricExecutable262,
		prune:   installationOutputPaths,
		prepare: append(
			runtimeInstallPreparation("26.2", "fabric@0.19.3"),
			initializeCommand{
				args:             []string{"init", "--force", "--no-interactive"},
				selectEntryPoint: fabric262Launcher,
			},
		),
		removeBefore: []string{fabric262Launcher},
		force:        true,
		minecraft:    "26.2", product: "fabric", coreVersion: "0.19.3",
		wantReceipt: true,
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.3",
			path: fabricShim,
			components: map[string]string{
				"minecraft:minecraft":  "26.2",
				"fabric:fabric-loader": "0.19.3",
			},
		},
	},

	// Without any remaining launcher for the declared identity, a missing
	// installation is a refusal, not an invitation to identify whatever
	// remains.
	"fabric-declared-installation-missing": {
		sandbox: fabricExecutable262,
		prepare: []initializeCommand{{
			args:             []string{"init", "--no-interactive"},
			selectEntryPoint: fabric262Launcher,
		}},
		removeBefore:  []string{fabric262Launcher},
		force:         true,
		expectFailure: true,
		minecraft:     "26.2", product: "fabric", coreVersion: "0.19.3",
		status: &statusExpectation{missing: true},
	},

	// The same replacement must still recognize the shim when the receipt no
	// longer owns it: a stale digest leaves the file unmanaged, but its
	// observed identity still matches the declaration. Ownership does not
	// decide which installation the declaration selects.
	"fabric-in-place-stale-ownership": {
		sandbox: fabricExecutable262,
		prune:   installationOutputPaths,
		prepare: append(
			runtimeInstallPreparation("26.2", "fabric@0.19.3"),
			initializeCommand{
				args:             []string{"init", "--force", "--no-interactive"},
				selectEntryPoint: fabric262Launcher,
			},
		),
		removeBefore: []string{fabric262Launcher},
		modifyFiles:  []string{fabricShim},
		force:        true,
		minecraft:    "26.2", product: "fabric", coreVersion: "0.19.3",
		wantReceipt: true,
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.3",
			path: fabricShim,
			components: map[string]string{
				"minecraft:minecraft":  "26.2",
				"fabric:fabric-loader": "0.19.3",
			},
		},
	},

	// A readable declaration and lock keep their identity when an unrelated,
	// genuinely different server jar appears beside them.
	"fabric-declared-survives-unrelated": {
		sandbox: fabricExecutable262,
		prepare: []initializeCommand{{
			args:             []string{"init", "--no-interactive"},
			selectEntryPoint: fabric262Launcher,
		}},
		artifacts: []initializeArtifact{{
			sandbox: fabricExecutable1214,
			source:  fabric1214Launcher,
			dest:    fabric1214Launcher,
		}},
		force:     true,
		minecraft: "26.2", product: "fabric", coreVersion: "0.19.3",
		informational: []string{fabric1214Launcher},
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.3",
			path: fabric262Launcher,
			components: map[string]string{
				"minecraft:minecraft":  "26.2",
				"fabric:fabric-loader": "0.19.3",
			},
		},
	},

	// Two real launchers with different loader and Minecraft identities stay a
	// conflict, and the refusal names the exact non-interactive selection.
	// Live status resolves the same tie deterministically for observation.
	"fabric-conflicting-identities": {
		sandbox: fabricExecutable262,
		artifacts: []initializeArtifact{{
			sandbox: fabricExecutable1214,
			source:  fabric1214Launcher,
			dest:    fabric1214Launcher,
		}},
		expectFailure: true,
		guidance:      []string{fabric262Launcher, fabric1214Launcher},
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.16.9",
			path: fabric1214Launcher,
			components: map[string]string{
				"minecraft:minecraft":  "1.21.4",
				"fabric:fabric-loader": "0.16.9",
			},
			warning: []string{fabric1214Launcher, fabric262Launcher},
		},
	},

	// --entry-point resolves that conflict without discarding the other jar.
	// The declaration keeps the chosen identity, so live status must keep
	// reporting the selected Fabric runtime rather than the preferred one.
	"fabric-conflicting-identities-selected": {
		sandbox: fabricExecutable262,
		artifacts: []initializeArtifact{{
			sandbox: fabricExecutable1214,
			source:  fabric1214Launcher,
			dest:    fabric1214Launcher,
		}},
		selectEntryPoint: fabric262Launcher,
		minecraft:        "26.2", product: "fabric", coreVersion: "0.19.3",
		informational: []string{fabric1214Launcher},
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.3",
			path: fabric262Launcher,
			components: map[string]string{
				"minecraft:minecraft":  "26.2",
				"fabric:fabric-loader": "0.19.3",
			},
		},
	},

	// A real Fabric launcher and a real vanilla server jar are two
	// installations. Init must still refuse, while live status prefers the mod
	// loader over plain Minecraft and reports the automatic choice.
	"fabric-vanilla-conflicting-identities": {
		sandbox: fabricExecutable262,
		artifacts: []initializeArtifact{{
			sandbox: vanilla262,
			source:  "server.jar",
			dest:    "server.jar",
		}},
		expectFailure: true,
		guidance:      []string{fabric262Launcher, "server.jar"},
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.3",
			path: fabric262Launcher,
			components: map[string]string{
				"minecraft:minecraft":  "26.2",
				"fabric:fabric-loader": "0.19.3",
			},
			warning: []string{fabric262Launcher, "server.jar"},
		},
	},

	// Explicit selection outranks the live-status preference: vanilla is
	// adopted even though observation would prefer the Fabric runtime, and
	// the declaration keeps that identity on later observation.
	"fabric-vanilla-conflicting-identities-selected": {
		sandbox: fabricExecutable262,
		artifacts: []initializeArtifact{{
			sandbox: vanilla262,
			source:  "server.jar",
			dest:    "server.jar",
		}},
		selectEntryPoint: "server.jar",
		minecraft:        "26.2", product: "minecraft", coreVersion: "26.2",
		informational: []string{fabric262Launcher},
		status: &statusExpectation{
			ecosystem: "minecraft", name: "minecraft", version: "26.2",
			path: "server.jar",
			components: map[string]string{
				"minecraft:minecraft": "26.2",
			},
		},
	},

	// A hybrid runtime outranks a plain mod loader during live observation.
	// Init still refuses the bootstrap ambiguity; Arclight's bundled Fabric
	// loader remains the reported component, not the copied meta launcher.
	"arclight-fabric-conflicting-identities": {
		sandbox: arclightFabric,
		artifacts: []initializeArtifact{{
			sandbox: cobblemon1211,
			source:  cobblemon1211Launcher,
			dest:    cobblemon1211Launcher,
		}},
		expectFailure: true,
		guidance:      []string{arclightFabricLauncher, cobblemon1211Launcher},
		status: &statusExpectation{
			name: "arclight", version: "arclight-1.21.1-1.0.1-8ec9529",
			path: arclightFabricLauncher,
			components: map[string]string{
				"minecraft:minecraft":  "1.21.1",
				"fabric:fabric-loader": "0.16.14",
			},
			warning: []string{arclightFabricLauncher, cobblemon1211Launcher},
		},
	},

	// A real legacy Forge universal jar at the server root is a standalone
	// runtime: its manifest carries the launch main class and no installer,
	// args, shim, or library layout is present.
	"forge-1122": {
		minecraft: "1.12.2", product: "forge", coreVersion: "14.23.5.2860",
		status: &statusExpectation{
			ecosystem: "forge", name: "forge", version: "14.23.5.2860",
			path: forge1122Jar,
			components: map[string]string{
				"minecraft:minecraft": "1.12.2",
				"forge:forge":         "14.23.5.2860",
			},
		},
	},

	// Real Forge and NeoForge installation layouts. Strict entry-point
	// semantics select validated launch args, except that a valid shim wins;
	// the fixture generator materializes the real installer output for these
	// rows, so the expected paths are the installed launch targets.
	"forge-1201": {
		minecraft: "1.20.1", product: "forge", coreVersion: "47.4.10",
		status: &statusExpectation{
			ecosystem: "forge", name: "forge", version: "47.4.10",
			path: forge1201EntryPoint,
			components: map[string]string{
				"minecraft:minecraft": "1.20.1",
				"forge:forge":         "47.4.10",
			},
		},
	},
	"forge-12111": {
		minecraft: "1.21.11", product: "forge", coreVersion: "61.2.0",
		status: &statusExpectation{
			ecosystem: "forge", name: "forge", version: "61.2.0",
			path: forge12111EntryPoint,
			components: map[string]string{
				"minecraft:minecraft": "1.21.11",
				"forge:forge":         "61.2.0",
			},
		},
	},
	"neoforge-211": {
		minecraft: "1.21.1", product: "neoforge", coreVersion: "21.1.248",
		status: &statusExpectation{
			ecosystem: "neoforge", name: "neoforge", version: "21.1.248",
			path: neoforge211EntryPoint,
			components: map[string]string{
				"minecraft:minecraft": "1.21.1",
				"neoforge:neoforge":   "21.1.248",
			},
		},
	},
	"neoforge-262": {
		minecraft: "26.2", product: "neoforge", coreVersion: "26.2.0.67",
		status: &statusExpectation{
			ecosystem: "neoforge", name: "neoforge", version: "26.2.0.67",
			path: neoforge262EntryPoint,
			components: map[string]string{
				"minecraft:minecraft": "26.2",
				"neoforge:neoforge":   "26.2.0.67",
			},
		},
	},

	// A real Paper jar and a server.jar symlink alias are one physical
	// installation. Explicit selection through the alias records the Paper
	// identity and the installed build; live status reports the canonical file
	// for that identity with the same build.
	"paper-symlink-alias": {
		sandbox: "paper",
		symlinks: []initializeSymlink{{
			target: paperJar,
			link:   "server.jar",
		}},
		selectEntryPoint: "server.jar",
		minecraft:        "1.21.11", product: "paper", coreVersion: "130",
		status: &statusExpectation{
			name: "paper", version: "130",
			path: paperJar,
			components: map[string]string{
				"minecraft:minecraft": "1.21.11",
			},
		},
	},

	// Two physically distinct copies of the same real Paper jar carry the same
	// build, so they describe one runtime. Init adopts the build instead of
	// treating a missing version as proof that they cannot merge.
	"paper-declared-ambiguous-twins": {
		sandbox: "paper",
		prepare: []initializeCommand{{
			args: []string{"init", "--no-interactive"},
		}},
		artifacts: []initializeArtifact{{
			source: paperJar,
			dest:   "paper-1.21.11-130-copy.jar",
		}},
		force:     true,
		minecraft: "1.21.11", product: "paper", coreVersion: "130",
		informational: []string{paperJar},
		status: &statusExpectation{
			name: "paper", version: "130",
			path: "paper-1.21.11-130-copy.jar",
			components: map[string]string{
				"minecraft:minecraft": "1.21.11",
			},
		},
	},

	// A real vanilla jar copied under Maven-shaped loader server paths must not
	// acquire Forge or NeoForge identity. The ordinary root jar stays selected.
	"vanilla-under-loader-server-jars": {
		sandbox: "vanilla-1201",
		artifacts: []initializeArtifact{
			{
				source: "server.jar",
				dest:   "libraries/net/minecraftforge/forge/1.21.1-52.0.28/forge-1.21.1-52.0.28-server.jar",
			},
			{
				source: "server.jar",
				dest:   "libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-server.jar",
			},
		},
		minecraft: "1.20.1", product: "minecraft", coreVersion: "1.20.1",
		status: &statusExpectation{
			ecosystem: "minecraft", name: "minecraft", version: "1.20.1",
			path: "server.jar",
			components: map[string]string{
				"minecraft:minecraft": "1.20.1",
			},
		},
	},

	// Two copies of the same real vanilla jar are one installation. The
	// ephemeral --entry-point choice names one copy, but the written lock
	// records only the runtime identity; later observation reports the
	// installation's lexical entry point, not the selected path.
	"vanilla-duplicate-jars": {
		sandbox: vanilla262,
		artifacts: []initializeArtifact{{
			source: "server.jar",
			dest:   "server-backup.jar",
		}},
		selectEntryPoint: "server.jar",
		minecraft:        "26.2", product: "minecraft", coreVersion: "26.2",
		status: &statusExpectation{
			ecosystem: "minecraft", name: "minecraft", version: "26.2",
			path: "server-backup.jar",
			components: map[string]string{
				"minecraft:minecraft": "26.2",
			},
		},
	},

	// The selected launcher disappears, but an unmanaged copy of the same
	// real runtime remains. The declared identity still matches it, so status
	// and init keep the server instead of reporting a missing installation.
	"vanilla-declared-unmanaged-survivor": {
		sandbox: vanilla262,
		prepare: []initializeCommand{{
			args: []string{"init", "--no-interactive"},
		}},
		artifacts: []initializeArtifact{{
			source: "server.jar",
			dest:   "server-backup.jar",
		}},
		removeBefore: []string{"server.jar"},
		force:        true,
		minecraft:    "26.2", product: "minecraft", coreVersion: "26.2",
		status: &statusExpectation{
			ecosystem: "minecraft", name: "minecraft", version: "26.2",
			path: "server-backup.jar",
			components: map[string]string{
				"minecraft:minecraft": "26.2",
			},
		},
	},

	// The declared Fabric runtime is gone and a different product remains.
	// Init and status must not adopt the vanilla jar as a substitute.
	"fabric-declared-missing-different-product": {
		sandbox: fabricExecutable262,
		prepare: []initializeCommand{{
			args: []string{"init", "--no-interactive"},
		}},
		artifacts: []initializeArtifact{{
			sandbox: vanilla262,
			source:  "server.jar",
			dest:    "server.jar",
		}},
		removeBefore:  []string{fabric262Launcher},
		force:         true,
		expectFailure: true,
		guidance:      []string{"server.jar"},
		minecraft:     "26.2", product: "fabric", coreVersion: "0.19.3",
		status: &statusExpectation{missing: true},
	},

	// The declared Fabric runtime is gone and a different loader/Minecraft
	// version remains. Version mismatch alone is enough to refuse adoption.
	"fabric-declared-missing-different-version": {
		sandbox: fabricExecutable262,
		prepare: []initializeCommand{{
			args: []string{"init", "--no-interactive"},
		}},
		artifacts: []initializeArtifact{{
			sandbox: fabricExecutable1214,
			source:  fabric1214Launcher,
			dest:    fabric1214Launcher,
		}},
		removeBefore:  []string{fabric262Launcher},
		force:         true,
		expectFailure: true,
		guidance:      []string{fabric1214Launcher},
		minecraft:     "26.2", product: "fabric", coreVersion: "0.19.3",
		status: &statusExpectation{missing: true},
	},
}
