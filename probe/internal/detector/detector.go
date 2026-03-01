package detector

import (
	"archive/zip"
	"os"

	"github.com/mclucy/lucy/types"
)

// ExecutableDetector is the interface for detecting different types of
// Minecraft servers
type ExecutableDetector interface {
	Detect(
		filePath string,
		zipReader *zip.Reader,
		fileHandle *os.File,
	) (*types.ExecutableInfo, error)
	Name() string
}

// PackageDetector is the interface for analyzing mods or plugins
type PackageDetector interface {
	Detect(zipReader *zip.Reader, fileHandle *os.File) ([]types.Package, error)
	Name() string
}

// EnvironmentDetector is the detector that handles the working directory to find
// environments external to the game runtime (or wraps it). E.g. git.
type EnvironmentDetector interface {
	Detect(dir string, env *types.EnvironmentInfo)
	Name() string
}
