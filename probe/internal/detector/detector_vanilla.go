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
	return "vanilla server"
}

func (d *VanillaDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*types.RuntimeInfo, error) {
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

			// This is to guard against misidentifying Forge installer jars as
			// vanilla servers, which also contain version.json but with different
			// structure
			forgeInstallerGuard := &struct {
				Comment   []string `json:"_comment"`
				MainClass string   `json:"mainClass"`
			}{}
			err = json.Unmarshal(data, forgeInstallerGuard)
			if err == nil {
				return types.NoExecutable, nil
			}

			obj := exttype.FileMinecraftVersionSpec{}
			err = json.Unmarshal(data, &obj)
			if err != nil {
				return nil, err
			}

			gameVersion := types.RawVersion(obj.Id)

			exec := &types.RuntimeInfo{
				PrimaryEntrance: filePath,
				GameVersion:     gameVersion,
				RuntimeIdentities: []types.PackageId{
					{
						Platform: types.PlatformMinecraft,
						Name:     "minecraft",
						Version:  gameVersion,
					},
				},
				Topology: &types.RuntimeTopology{
					PrimaryNode: "minecraft",
					Nodes: []types.RuntimeNode{
						{
							ID:               "minecraft",
							Role:             types.RuntimeRoleVanilla,
							IdentityPlatform: types.PlatformMinecraft,
						},
					},
				},
			}

			return exec, nil
		}
	}

	return nil, nil
}

func init() {
	registerExecutableDetector(&VanillaDetector{})
}
