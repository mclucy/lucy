// Package help holds the user-facing description of every subcommand.
//
// One package owns the words so help promises are reviewed and edited
// against the whole help surface, and a command cannot drift its own
// description silently.
package help

const (
	AddShort = "Add packages"

	BisectShort       = "Find a problematic package with binary search"
	BisectStartShort  = "Start a binary-search session"
	BisectGoodShort   = "Mark current midpoint as good"
	BisectBadShort    = "Mark current midpoint as bad"
	BisectStatusShort = "Show the active bisect session"
	BisectResetShort  = "Abort the active bisect session and restore the manifest"

	CacheShort      = "Manage the network cache"
	CacheLsShort    = "List cache entries"
	CacheClearShort = "Clear all cache"

	CompileShort = "Compile a Fabric, Forge, or NeoForge mod from a Git repository"
	CompileLong  = "Clone a Git repository into a temporary directory and compile a Forge, NeoForge, or Fabric mod. Requires local Java toolchain. " +
		"Compilation executes repository build scripts and can download build " +
		"dependencies. Run this command only on the repository you trust."

	ConfigShort = "Manage lucy's configurations"

	CreateShort = "Create a new server"

	DoctorShort = "Diagnose the server environment"

	ExportShort = "Export server configuration, or generate client"

	InfoShort = "Display information of a package"
	InfoLong  = "Pass a local .jar, .zip, .pyz, or .mcdr file or specify a " +
		"package identifier. When no registry is specified, all registries are queried"

	InitShort = "Generate a manifest file for an existing server directory"

	InstallShort = "Sync the server with the manifest and lock"

	JavaShort     = "Show and choose the Java runtime of the server"
	JavaListShort = "List installed Java runtimes and the Java the server asks for"
	JavaUseShort  = "Pin a Java version or distribution, or use a Java runtime on this machine"
	JavaUseLong   = "A version or --distribution pins the Java of the server in lucy.yaml. " +
		"auto returns that part to the default: the Java version that Mojang " +
		"recommends for the Minecraft version, from any distribution. --path " +
		"uses one Java home or java executable on this machine only and records " +
		"it in lucy.local.yaml, which takes precedence over lucy.yaml; " +
		"--path auto lets Lucy choose again. Without arguments, an " +
		"interactive form asks for the version and the distribution."

	LeavesShort = "List packages with no dependencies"

	RemoveShort = "Remove packages"

	SearchShort = "Search for mods and plugins"

	StatusShort = "Display basic information of the current server"

	TreeShort = "Show all packages and their dependencies"

	UpgradeShort = "Upgrade installed packages"
)
