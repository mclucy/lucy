//go:build e2e

package init

const (
	cobblemon1211        = "cobblemon-1211"
	fabricExecutable262  = "fabric-executable-262"
	fabricExecutable1214 = "fabric-executable-1214"
	vanilla262           = "vanilla-262"

	cobblemon1211Launcher = "fabric-server-mc.1.21.1-loader.0.19.5-launcher.1.1.2.jar"
	fabric262Launcher     = "fabric-server-mc.26.2-loader.0.19.3-launcher.1.1.2.jar"
	fabric1214Launcher    = "fabric-server-mc.1.21.4-loader.0.16.9-launcher.1.0.1.jar"
	fabricShim            = "fabric-server-launch.jar"
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
	// wantEntryPoint is the lock entry point after init, or after a refusal
	// that must not change the declaration.
	wantEntryPoint string
	// wantReceipt keeps the installation receipt assertion; init itself never
	// stages one.
	wantReceipt bool
	// status asserts the live server identity after a successful init.
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
	args             []string
	selectEntryPoint string
	wantEntryPoint   string
}

// initializeArtifact copies one pinned file from a generated sandbox.
type initializeArtifact struct {
	// sandbox is the fixture that holds the file; the case sandbox when empty.
	sandbox string
	source  string
	dest    string
}

// statusExpectation describes the server identity `status --json-compact`
// reports. components maps "ecosystem:name" to the expected version.
type statusExpectation struct {
	ecosystem  string
	name       string
	version    string
	components map[string]string
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
		wantEntryPoint: fabricShim,
		wantReceipt:    true,
		unchanged:      []string{cobblemon1211Launcher},
		informational:  []string{cobblemon1211Launcher},
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.5",
			components: map[string]string{
				"minecraft:minecraft":  "1.21.1",
				"fabric:fabric-loader": "0.19.5",
			},
		},
	},

	// Installation replaces the declared meta launcher with its managed shim.
	// Removing the declared entry point may only fall back to a receipt-owned
	// entry point of the same identity.
	"fabric-in-place-managed-replacement": {
		sandbox: fabricExecutable262,
		prune:   installationOutputPaths,
		prepare: append(
			runtimeInstallPreparation("26.2", "fabric@0.19.3"),
			initializeCommand{
				args:             []string{"init", "--force", "--no-interactive"},
				selectEntryPoint: fabric262Launcher,
				wantEntryPoint:   fabric262Launcher,
			},
		),
		removeBefore: []string{fabric262Launcher},
		force:        true,
		minecraft:    "26.2", product: "fabric", coreVersion: "0.19.3",
		wantEntryPoint: fabricShim,
		wantReceipt:    true,
	},

	// Without a managed replacement, a missing declared entry point is a
	// refusal, not an invitation to identify whatever remains.
	"fabric-declared-entry-point-missing": {
		sandbox: fabricExecutable262,
		prepare: []initializeCommand{{
			args:             []string{"init", "--no-interactive"},
			selectEntryPoint: fabric262Launcher,
			wantEntryPoint:   fabric262Launcher,
		}},
		removeBefore:  []string{fabric262Launcher},
		force:         true,
		expectFailure: true,
		minecraft:     "26.2", product: "fabric", coreVersion: "0.19.3",
		wantEntryPoint: fabric262Launcher,
	},

	// The same replacement must refuse when the receipt no longer owns the
	// shim: a stale digest leaves the file unmanaged, and the declared entry
	// point is gone.
	"fabric-in-place-stale-ownership": {
		sandbox: fabricExecutable262,
		prune:   installationOutputPaths,
		prepare: append(
			runtimeInstallPreparation("26.2", "fabric@0.19.3"),
			initializeCommand{
				args:             []string{"init", "--force", "--no-interactive"},
				selectEntryPoint: fabric262Launcher,
				wantEntryPoint:   fabric262Launcher,
			},
		),
		removeBefore:  []string{fabric262Launcher},
		modifyFiles:   []string{fabricShim},
		force:         true,
		expectFailure: true,
		minecraft:     "26.2", product: "fabric", coreVersion: "0.19.3",
		wantEntryPoint: fabric262Launcher,
		wantReceipt:    true,
	},

	// A readable declaration and lock keep their identity when an unrelated,
	// genuinely different server jar appears beside them.
	"fabric-declared-survives-unrelated": {
		sandbox: fabricExecutable262,
		prepare: []initializeCommand{{
			args:             []string{"init", "--no-interactive"},
			selectEntryPoint: fabric262Launcher,
			wantEntryPoint:   fabric262Launcher,
		}},
		artifacts: []initializeArtifact{{
			sandbox: fabricExecutable1214,
			source:  fabric1214Launcher,
			dest:    fabric1214Launcher,
		}},
		force:     true,
		minecraft: "26.2", product: "fabric", coreVersion: "0.19.3",
		wantEntryPoint: fabric262Launcher,
		informational:  []string{fabric1214Launcher},
		status: &statusExpectation{
			ecosystem: "fabric", name: "fabric", version: "0.19.3",
			components: map[string]string{
				"minecraft:minecraft":  "26.2",
				"fabric:fabric-loader": "0.19.3",
			},
		},
	},

	// Two real launchers with different loader and Minecraft identities stay a
	// conflict, and the refusal names the exact non-interactive selection.
	"fabric-conflicting-identities": {
		sandbox: fabricExecutable262,
		artifacts: []initializeArtifact{{
			sandbox: fabricExecutable1214,
			source:  fabric1214Launcher,
			dest:    fabric1214Launcher,
		}},
		expectFailure: true,
		guidance:      []string{fabric262Launcher, fabric1214Launcher},
	},

	// --entry-point resolves that conflict without discarding the other jar.
	"fabric-conflicting-identities-selected": {
		sandbox: fabricExecutable262,
		artifacts: []initializeArtifact{{
			sandbox: fabricExecutable1214,
			source:  fabric1214Launcher,
			dest:    fabric1214Launcher,
		}},
		selectEntryPoint: fabric262Launcher,
		minecraft:        "26.2", product: "fabric", coreVersion: "0.19.3",
		wantEntryPoint: fabric262Launcher,
	},

	// Two copies of the same real vanilla jar are one installation.
	"vanilla-duplicate-jars": {
		sandbox: vanilla262,
		artifacts: []initializeArtifact{{
			source: "server.jar",
			dest:   "server-backup.jar",
		}},
		minecraft: "26.2", product: "minecraft", coreVersion: "26.2",
		wantEntryPoint: "server-backup.jar",
	},
}
