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

	LeavesShort = "List packages with no dependencies"

	RemoveShort = "Remove packages"

	SearchShort = "Search for mods and plugins"

	StatusShort = "Display basic information of the current server"

	TreeShort = "Show all packages and their dependencies"

	UpgradeShort = "Upgrade installed packages"
)
