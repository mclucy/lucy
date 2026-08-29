package detector

import (
	"archive/zip"
	"os"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

// VanillaDetector detects vanilla Minecraft servers
type VanillaDetector struct{}

func (d *VanillaDetector) Name() string {
	return "vanilla server"
}

func (d *VanillaDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	data, ok, err := readArchiveEntry(zipReader, fabricLaunchPropertiesPath)
	if err != nil {
		return nil, err
	}
	if ok && fabricLauncherPropertiesAreServer(data) {
		return nil, nil
	}

	for _, f := range zipReader.File {
		if f.Name == mojangVersionJSONEntry {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer fn.CloseReader(r, log.Warn)

			data, err := fsutil.CopyBytes(r, fsutil.MaxZipEntryBytes)
			if err != nil {
				return nil, err
			}

			gameVersion, guarded, err := mojangVersionFromJSON(data)
			if err != nil {
				return nil, err
			}
			if guarded {
				return nil, nil
			}

			exec := &ExecutableEvidence{PrimaryPath: filePath, PrimaryRuntime: &types.VersionedPackageRef{
				Eco:     types.EcoMinecraft,
				Name:    "minecraft",
				Version: gameVersion,
			}, RuntimeComponents: []types.VersionedPackageRef{
				{
					Eco:     types.EcoMinecraft,
					Name:    "minecraft",
					Version: gameVersion,
				},
			}}

			return exec, nil
		}
	}

	return nil, nil
}

func init() {
	registerExecutableDetector(&VanillaDetector{})
}
