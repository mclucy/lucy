package artifact

import "archive/zip"

// Reader extracts metadata from supported artifact archives.
type Reader interface {
	// Read accepts a nil resolver to leave detected names unchanged.
	Read(r *zip.Reader, filePath string, resolver SlugResolver) ([]Info, error)
}

// readers is the explicit ordered list of all platform readers.
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
