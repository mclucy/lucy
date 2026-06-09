package artifact

import "archive/zip"

// Reader extracts artifact metadata from a ZIP reader.
// Each platform has its own Reader implementation.
type Reader interface {
	// Read analyzes the ZIP contents and returns detected artifacts.
	// The resolver parameter may be nil if no slug resolver was configured.
	Read(r *zip.Reader, filePath string, resolver SlugResolver) ([]ArtifactInfo, error)
}

// readers is the explicit ordered list of all platform readers.
// Populated by each reader file which provides a constructor.
// Order matters: earlier readers take priority when multiple match.
var readers = []Reader{
	newFabricReader(),
	newForgeReader(),
	newForgeLegacyReader(),
	newNeoforgeReader(),
	newBukkitReader(),
	newVelocityReader(),
	newBungeeCordReader(),
	newSpongeReader(),
	newMcdrReader(),
}
