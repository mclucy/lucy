package detector

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

// VanillaDetector detects vanilla Minecraft servers
type VanillaDetector struct{}

func (d *VanillaDetector) Name() string {
	// TODO implement me
	panic("implement me")
}

func (d *VanillaDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*types.ExecutableInfo, error) {
	for _, f := range zipReader.File {
		if f.Name == "version.json" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer tools.CloseReader(r, logger.Warn)

			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			obj := exttype.FileMinecraftVersionSpec{}
			err = json.Unmarshal(data, &obj)
			if err != nil {
				return nil, err
			}

			exec := &types.ExecutableInfo{
				Path:        filePath,
				ModLoader:   types.PlatformMinecraft,
				GameVersion: types.RawVersion(obj.Id),
			}

			return exec, nil
		}
	}

	return nil, nil
}

func init() {
	registerExecutableDetector(&VanillaDetector{})
}
