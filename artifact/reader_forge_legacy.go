package artifact

import (
	"archive/zip"
	"encoding/json"
	"io"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/types"
)

type forgeLegacyReader struct{}

func newForgeLegacyReader() Reader {
	return &forgeLegacyReader{}
}

func (r *forgeLegacyReader) Read(
	zipRdr *zip.Reader,
	filePath string,
	resolver SlugResolver,
) ([]Info, error) {
	var raw []byte
	for _, f := range zipRdr.File {
		if f.Name != "mcmod.info" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		raw, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		break
	}

	if raw == nil {
		return nil, nil
	}

	var mods fileschema.FileForgeModIdentifierOld
	if err := json.Unmarshal(raw, &mods); err != nil {
		return nil, err
	}

	infos := make([]Info, 0, len(mods))
	for _, m := range mods {
		if m.ModId == "forge" || m.ModId == "minecraft" || m.ModId == "mcp" {
			continue
		}

		info := Info{
			Ref: types.PackageRef{
				Eco:  types.EcoForge,
				Name: input.ToProjectName(m.ModId),
			},
			Version:  types.BareVersion(m.Version),
			FilePath: filePath,
		}

		if len(m.Dependencies) > 0 {
			deps := make([]Dependency, 0, len(m.Dependencies))
			for _, rawDep := range m.Dependencies {
				depStr, ok := rawDep.(string)
				if !ok || depStr == "" {
					continue
				}
				deps = append(
					deps, Dependency{
						Ref: types.PackageRef{
							Eco:  types.EcoForge,
							Name: input.ToProjectName(depStr),
						},
						Mandatory: true,
					},
				)
			}
			if len(deps) > 0 {
				info.Dependencies = deps
			}
		}

		infos = append(infos, info)
	}

	return infos, nil
}
