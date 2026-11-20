package artifact

import "archive/zip"

// Reader extracts metadata from supported artifact archives.
type Reader interface {
	Read(r *zip.Reader, filePath string) ([]PackageInfo, error)
}

type inputReader interface {
	readWithInputs(r *zip.Reader, filePath string, inputs *descriptorInputs) ([]PackageInfo, error)
}

// readers is the explicit-ordered list of all platform readers.
// Order matters: earlier readers take priority when multiple match.
var readers = []Reader{
	newQuiltReader(),
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
