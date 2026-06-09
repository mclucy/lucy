package detector

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"

	"github.com/mclucy/lucy/logger"
)

func init() {
	registerOtherPackageDetector(&McdrPluginDetector{})
}

type McdrPluginDetector struct{}

func (d *McdrPluginDetector) Name() string {
	return "mcdr plugin"
}

func (d *McdrPluginDetector) Detect(
	zipReader *zip.Reader,
	fileHandle *os.File,
) (packages []types.Package, err error) {
	var pkg types.Package
	for _, f := range zipReader.File {
		if f.Name == "mcdreforged.plugin.json" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer tools.CloseReader(r, logger.Warn)

			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}
			pluginInfo := &exttype.FileMcdrPluginIdentifier{}
			if err := json.Unmarshal(data, pluginInfo); err != nil {
				return nil, err
			}

			pkg = translateMcdrPlugin(pluginInfo, fileHandle.Name())
		}
	}

	packages = append(packages, pkg)
	return packages, nil
}
