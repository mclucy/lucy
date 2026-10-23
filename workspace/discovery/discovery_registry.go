package discovery

// recognizer scans the shared root view and returns complete installations plus
// typed diagnostics. Recognizers are private: the only registration surface is
// the static ordered registry below.
type recognizer func(*scan) ([]Installation, []Finding)

type recognizerEntry struct {
	name     string
	discover recognizer
}

// recognizerRegistry is the one deterministic directory-recognition order.
// There is no init registration and no separate root executable fallback loop.
var recognizerRegistry = []recognizerEntry{
	{name: "fabric", discover: discoverFabric},
	{name: "forge", discover: discoverForge},
	{name: "neoforge", discover: discoverNeoForge},
	{name: "bukkit", discover: discoverBukkit},
	{name: "arclight", discover: discoverArclight},
	{name: "catserver", discover: discoverCatServer},
	{name: "sponge", discover: discoverSponge},
	{name: "geyser", discover: discoverGeyser},
	{name: "vanilla", discover: discoverVanilla},
}
