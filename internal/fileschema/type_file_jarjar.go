package fileschema

// FileJarjar represents the contents of META-INF/jarjar/metadata.json
// embedded inside a Forge or NeoForge mod JAR. This file describes library dependencies
// that the mod author chose to bundle (shade) directly inside their JAR using
// the JarJar mechanism.
//
// Reference: https://docs.neoforged.net/toolchain/docs/dependencies/jarinjar/
type FileJarjar struct {
	Jars []JarjarEntry `json:"jars"`
}

// JarjarEntry describes a single embedded library inside a Forge or NeoForge
// mod JAR's META-INF/jarjar/ directory.
type JarjarEntry struct {
	Identifier JarjarIdentifier `json:"identifier"`

	// Version holds both the version range the mod is compatible with and
	// the exact version actually bundled.
	Version JarjarVersion `json:"version"`

	Path string `json:"path"`

	IsObfuscated bool `json:"isObfuscated"`
}

// JarjarIdentifier is the Maven coordinate group+artifact pair for an
// embedded JarInJar library.
type JarjarIdentifier struct {
	Group    string `json:"group"`
	Artifact string `json:"artifact"`
}

// JarjarVersion carries the version range constraint and the concrete
// bundled version for a JarInJar entry.
//
// Range uses Maven version range syntax, e.g. "[1.0,2.0)" means 1.0 ≤ x < 2.0.
// ArtifactVersion is the version string of the JAR actually bundled.
type JarjarVersion struct {
	Range           string `json:"range"`
	ArtifactVersion string `json:"artifactVersion"`
}
