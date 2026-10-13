// Package message holds the user-facing description of every subcommand.
//
// One package owns the words so help promises re reviewed and edited
// against the whole help surface, and a command cannot drift its own
// description silently.
package message

const (
	AddShort = "Add packages under explicit operator control"

	BisectShort       = "Find a problematic package by binary search"
	BisectStartShort  = "Start a binary-search session"
	BisectGoodShort   = "Mark current midpoint as good (bad package is in right half)"
	BisectBadShort    = "Mark current midpoint as bad (bad package is in the left half)"
	BisectStatusShort = "Show the active bisect session"
	BisectResetShort  = "Abort the active bisect session and restore the manifest"

	CacheShort      = "Manage the download cache"
	CacheLsShort    = "List cached entries"
	CacheClearShort = "Clear all cached downloads"

	CompileShort = "Compile a Minecraft mod or plugin from a Git repository"
	CompileLong  = "Clone a Git repository into a temporary directory and compile a Forge, NeoForge, or Fabric mod. Requires local Java toolchain." +
		"Compilation executes repository build scripts and can download build dependencies. Run this command only on the repository you trust."

	ConfigShort = "Manage lucy's configurations"

	CreateShort = "Create a lucy.yaml"

	DoctorShort = "Diagnose the server environment"

	ExportShort = "Export server configuration, or generate client"

	InfoShort = "Display information of a package"
	InfoLong  = `Display information of a mod or plugin from an upstream source.

Pass a local .jar, .zip, .pyz, or .mcdr file or specify a package identifier.`

	InitShort = "Generate a manifest file for an existing server directory"

	InstallShort = "Install packages"

	LeavesShort = "List leaf packages (packages with no dependents)"

	RemoveShort = "Remove packages under explicit operator control"

	SearchShort = "Search for mods and plugins"

	StatusShort = "Display basic information of the current server"

	TreeShort = "Show the resolved package dependency tree"

	UpgradeShort = "Upgrade installed packages"
)
