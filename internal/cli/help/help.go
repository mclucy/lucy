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

	ConfigShort       = "Manage lucy's configurations"
	ConfigGetShort    = "Show a configuration value or the effective configuration"
	ConfigSetShort    = "Set a configuration value"
	ConfigCreateShort = "Create a commented configuration file"

	CreateShort = "Create a new server"
	CreateLong  = "Create a new server in a directory. The interactive form asks " +
		"where the server lives, which software it runs, its Minecraft and " +
		"software versions, and whether to install it now. Use --no-install " +
		"to write only the manifest and lock for a later lucy install."

	DeinitShort = "Remove lucy from the current server directory"
	DeinitLong  = "Delete lucy.yaml, lucy-lock.yaml, and the .lucy directory " +
		"from the current directory, including workspace configuration. The server, its packages, " +
		"worlds, and configuration stay in place, so it keeps running without " +
		"lucy. Files that lucy installed become unmanaged."

	DocsShort     = "Read the documentation"
	DocsListShort = "List documentation pages"
	DocsGrepShort = "Search documentation pages"
	DocsGrepLong  = "Search embedded documentation pages. Patterns use Go regular " +
		"expressions unless --fixed-strings is set. Exit status is 0 when a line " +
		"matches, 1 when no line matches, and 2 on error."
	DocsReadShort = "Print documentation pages"

	DoctorShort = "Diagnose the server environment"

	ExportShort = "Export server configuration, or generate client"

	InfoShort = "Display information of a package"
	InfoLong  = "Pass a local .jar, .zip, .pyz, or .mcdr file or specify a " +
		"package identifier. When no registry is specified, all registries are queried"

	InitShort = "Generate a manifest file for an existing server directory"

	InstallShort = "Sync the server with the manifest and lock"

	JavaShort     = "Show and choose the Java runtime of the server"
	JavaListShort = "List installed Java runtimes and the Java the server asks for"
	JavaUseShort  = "Declare a Java version or distribution"
	JavaUseLong   = "A version or --distribution pins the Java requirements in lucy.yaml. " +
		"auto returns that part to the default: the Java version that Mojang " +
		"recommends for the Minecraft version, from any distribution. " +
		"Without arguments, an interactive form asks for the version and distribution. " +
		"Run lucy config set java.home <home> to select a runtime on this machine."

	LeavesShort = "List packages with no dependencies"

	RemoveShort = "Remove packages"

	SearchShort = "Search for mods and plugins"

	StatusShort = "Display basic information of the current server"

	TreeShort = "Show all packages and their dependencies"

	UpgradeShort = "Upgrade installed packages"
)
