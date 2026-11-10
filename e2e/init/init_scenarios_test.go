//go:build e2e

package init

const (
	cobblemon1211        = "cobblemon-1211"
	fabricExecutable262  = "fabric-executable-262"
	fabricExecutable1214 = "fabric-executable-1214"
	quiltModpack1201     = "quilt-modpack-1201"
	vanilla1201          = "vanilla-1201"

	cobblemon1211Launcher = "fabric-server-mc.1.21.1-loader.0.19.5-launcher.1.1.2.jar"
	fabric262Launcher     = "fabric-server-mc.26.2-loader.0.19.3-launcher.1.1.2.jar"
	fabric1214Launcher    = "fabric-server-mc.1.21.4-loader.0.16.9-launcher.1.0.1.jar"
	fabricShim            = "fabric-server-launch.jar"
	quiltLauncher         = "quilt-server-launch.jar"
	quiltLauncherProps    = "quilt-server-launcher.properties"
	paperJar              = "paper-1.21.11-130.jar"
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
	// selectCore, selectCoreVersion, and selectMinecraft narrow the adopted
	// installation for this invocation only.
	selectCore        string
	selectCoreVersion string
	selectMinecraft   string
	// expectFailure requires init to refuse the directory.
	expectFailure bool
	// guidance lists exact selection commands a refusal must print.
	guidance []string
	// wantReceipt requires the installation receipt that records the packages
	// init identified from upstream metadata.
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
type initializeCommand struct {
	args []string
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
	// warning lists identity labels a live status diagnostic must name.
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

// quiltInstallationOutputPaths are the files the Quilt adapter owns. They are
// removed before a composition case prepares its own runtime, so a reused
// sandbox never supplies runtime identity that the case did not generate.
var quiltInstallationOutputPaths = []string{
	".lucy/install.yaml",
	".lucy/writer.lock",
	"lucy.yaml",
	"lucy-lock.yaml",
	quiltLauncher,
	quiltLauncherProps,
	"libraries",
	"server.jar",
	"versions",
	"cache",
}

// runtimeInstallPreparation declares a core-only runtime and installs it, so
// the case exercises the launcher seam without adopting the fixture's mods.
func runtimeInstallPreparation(minecraft, core string) []initializeCommand {
	return []initializeCommand{
		{args: []string{
			"create", "--minecraft", minecraft, "--core", core, "--no-install", "--no-interactive",
		}},
		{args: []string{"install", "--locked", "--overwrite", "--no-interactive"}},
	}
}

// initializeScenarios keeps the real-artifact initialization coverage. Each
// case proves an identity that only the shipped artifacts can establish:
// modpack package identification, an installed loader layout, hybrid
// components, a canonicalized alias, or a vanilla jar that must not acquire
// loader identity. Synthetic layouts, selection transitions, manifests, locks,
// receipts, unchanged files, failure guidance, and live-status projection are
// covered in-process by operation, workspace/discovery, workspace, and
// internal/cli.
var initializeScenarios = map[string]initializeScenario{
	// A real Fabric modpack: launcher identity, catalogue identification of its
	// mods, and omission of client-only jars.
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
		wantReceipt: true,
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.5",
			components: map[string]string{
				"minecraft:minecraft":  "1.21.1",
				"fabric:fabric-loader": "0.19.5",
			},
		},
	},
	// A real NeoForge modpack with a large catalogue-identified package set.
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
		wantReceipt: true,
		status: &statusExpectation{
			ecosystem: "neoforge", version: "21.1.233",
			components: map[string]string{
				"minecraft:minecraft": "1.21.1",
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
		wantReceipt: true,
		unchanged:   []string{cobblemon1211Launcher},
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.5",
			components: map[string]string{
				"minecraft:minecraft":  "1.21.1",
				"fabric:fabric-loader": "0.19.5",
			},
		},
	},

	// A real Quilt modpack whose index declares quilt-loader 0.20.0. The case
	// installs that exact loader into the materialized pack, deletes the
	// declaration and receipt, then proves init recovers the Quilt identity and
	// translates the pack's server-side mods without disturbing the pack files.
	// The pinned pack has 26 mods; 12 declare server_side unsupported in the
	// Modrinth catalogue and the artifact readers omit their client-only
	// descriptors, so exactly 14 server-deployable packages remain.
	"quilt-modpack-1201": {
		sandbox:      quiltModpack1201,
		prune:        quiltInstallationOutputPaths,
		prepare:      runtimeInstallPreparation("1.20.1", "quilt@0.20.0"),
		removeBefore: []string{"lucy.yaml", "lucy-lock.yaml", ".lucy"},
		minecraft:    "1.20.1", product: "quilt", coreVersion: "0.20.0",
		minPackages: 14,
		packages: map[string]map[string]string{
			"fabric": {
				"modrinth:lithium":   "mc1.20.1-0.11.2",
				"modrinth:modernfix": "5.7.1+mc1.20.1",
			},
			"quilt": {
				"modrinth:qsl": "7.1.2+0.87.0-1.20.1",
			},
		},
		absent: map[string][]string{
			"fabric": {
				"modrinth:betterf3",
				"modrinth:dashloader",
				"modrinth:dynamic-fps",
				"modrinth:entityculling",
				"modrinth:hold-that-chunk",
				"modrinth:indium",
				"modrinth:language-reload",
				"modrinth:ok-zoomer",
				"modrinth:reeses-sodium-options",
				"modrinth:rrls",
				"modrinth:sodium",
				"modrinth:sodium-extra",
			},
			"quilt": {
				"modrinth:betterf3",
				"modrinth:dashloader",
				"modrinth:dynamic-fps",
				"modrinth:entityculling",
				"modrinth:hold-that-chunk",
				"modrinth:indium",
				"modrinth:language-reload",
				"modrinth:ok-zoomer",
				"modrinth:reeses-sodium-options",
				"modrinth:rrls",
				"modrinth:sodium",
				"modrinth:sodium-extra",
			},
		},
		wantReceipt: true,
		unchanged:   []string{"mods/lithium-fabric-mc1.20.1-0.11.2.jar"},
		status: &statusExpectation{
			ecosystem: "quilt", name: "quilt", version: "0.20.0",
			path: quiltLauncher,
			components: map[string]string{
				"minecraft:minecraft": "1.20.1",
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
		guidance: []string{
			"lucy init --core fabric --core-version 0.19.3 --minecraft 26.2",
			"lucy init --core fabric --core-version 0.16.9 --minecraft 1.21.4",
		},
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.16.9",
			components: map[string]string{
				"minecraft:minecraft":  "1.21.4",
				"fabric:fabric-loader": "0.16.9",
			},
			warning: []string{
				"Fabric 0.19.3 / Minecraft 26.2",
				"Fabric 0.16.9 / Minecraft 1.21.4",
			},
		},
	},

	// Real Forge and NeoForge installation layouts. The fixture generator
	// materializes the real installer output for these rows, so the expected
	// paths are the installed launch targets rather than a root jar.
	"forge-1201": {
		minecraft: "1.20.1", product: "forge", coreVersion: "47.4.10",
		status: &statusExpectation{
			ecosystem: "forge", name: "forge", version: "47.4.10",
			components: map[string]string{
				"minecraft:minecraft": "1.20.1",
				"forge:forge":         "47.4.10",
			},
		},
	},
	"neoforge-262": {
		minecraft: "26.2", product: "neoforge", coreVersion: "26.2.0.67",
		status: &statusExpectation{
			ecosystem: "neoforge", name: "neoforge", version: "26.2.0.67",
			components: map[string]string{
				"minecraft:minecraft": "26.2",
				"neoforge:neoforge":   "26.2.0.67",
			},
		},
	},

	// A real Paper jar and a server.jar symlink alias are one physical
	// installation. The core filter records the Paper identity and the
	// installed build.
	"paper-symlink-alias": {
		sandbox: "paper",
		symlinks: []initializeSymlink{{
			target: paperJar,
			link:   "server.jar",
		}},
		selectCore: "paper",
		minecraft:  "1.21.11", product: "paper", coreVersion: "130",
		status: &statusExpectation{
			name: "paper", version: "130",
			components: map[string]string{
				"minecraft:minecraft": "1.21.11",
			},
		},
	},

	// A real vanilla jar copied under Maven-shaped loader server paths must not
	// acquire Forge or NeoForge identity. The ordinary root jar stays selected.
	"vanilla-under-loader-server-jars": {
		sandbox: vanilla1201,
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
			components: map[string]string{
				"minecraft:minecraft": "1.20.1",
			},
		},
	},
}
