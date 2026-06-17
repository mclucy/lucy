package detector

import (
	"archive/zip"
	"os"
)

// ExecutableDetector is the interface for detecting different types of
// Minecraft servers
type ExecutableDetector interface {
	Detect(
		filePath string,
		zipReader *zip.Reader,
		fileHandle *os.File,
	) (*ExecutableEvidence, error)
	Name() string
}
