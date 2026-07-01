package detector

import (
	"archive/zip"
	"encoding/json"
	"os"

	"github.com/mclucy/lucy/internal/fileschema"
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
		if f.Name == "version.json" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer fn.CloseReader(r, log.Warn)

			data, err := fsutil.CopyBytes(r, fsutil.MaxZipEntryBytes)
			if err != nil {
				return nil, err
			}

			// Guard against Forge installer jars, which also contain version.json
			// but with _comment and/or mainClass. encoding/json ignores unknown
			// fields, so we must check that the guard fields are populated.
			forgeInstallerGuard := &struct {
				Comment   []string `json:"_comment"`
				MainClass string   `json:"mainClass"`
			}{}
			if err := json.Unmarshal(data, forgeInstallerGuard); err == nil &&
				(len(forgeInstallerGuard.Comment) > 0 ||
					forgeInstallerGuard.MainClass != "") {
				return nil, nil
			}

			obj := fileschema.FileMinecraftVersionSpec{}
			err = json.Unmarshal(data, &obj)
			if err != nil {
				return nil, err
			}

			gameVersion := types.BareVersion(obj.Id)

			exec := &ExecutableEvidence{
				PrimaryEntrance: filePath,
				GameVersion:     gameVersion,
				RuntimeIdentities: []types.VersionedPackageRef{
					{
						PackageRef: types.PackageRef{
							Eco:  types.EcoMinecraft,
							Name: "minecraft",
						},
						Version: gameVersion,
					},
				},
				Topology: &types.ServerTopology{
					PrimaryNode: "minecraft",
					Nodes: []types.RuntimeNode{
						{
							ID:   "minecraft",
							Role: types.RuntimeRoleVanilla,
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
