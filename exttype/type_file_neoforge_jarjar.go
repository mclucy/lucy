package exttype

// FileNeoforgeJarjar represents the contents of META-INF/jarjar/metadata.json
// embedded inside a NeoForge mod JAR. This file describes library dependencies
// that the mod author chose to bundle (shade) directly inside their JAR using
// the NeoForge JarInJar mechanism.
//
// Reference: https://docs.neoforged.net/toolchain/docs/dependencies/jarinjar/
type FileNeoforgeJarjar struct {
	Jars []NeoforgeJarjarEntry `json:"jars"`
}

// NeoforgeJarjarEntry describes a single embedded library inside a NeoForge
// mod JAR's META-INF/jarjar/ directory.
type NeoforgeJarjarEntry struct {
	// Identifier holds the Maven group and artifact ID of the bundled library.
	Identifier NeoforgeJarjarIdentifier `json:"identifier"`

	// Version holds the version range the mod is compatible with and the
	// exact version actually bundled.
	Version NeoforgeJarjarVersion `json:"version"`

	// Path is the path inside the JAR to the embedded library JAR file,
	// e.g. "META-INF/jarjar/flywheel-neoforge-1.21.1-1.0.6.jar".
	Path string `json:"path"`

	// IsObfuscated indicates whether the embedded JAR has been obfuscated.
	IsObfuscated bool `json:"isObfuscated"`
}

// NeoforgeJarjarIdentifier is the Maven coordinate group+artifact pair for an
// embedded JarInJar library.
type NeoforgeJarjarIdentifier struct {
	Group    string `json:"group"`
	Artifact string `json:"artifact"`
}

// NeoforgeJarjarVersion carries the version range constraint and the concrete
// bundled version for a JarInJar entry.
//
// Range uses Maven version range syntax, e.g. "[1.0,2.0)" means 1.0 ≤ x < 2.0.
// ArtifactVersion is the version string of the JAR actually bundled.
type NeoforgeJarjarVersion struct {
	Range           string `json:"range"`
	ArtifactVersion string `json:"artifactVersion"`
}
