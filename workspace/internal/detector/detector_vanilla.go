package detector

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
)

type VanillaDetector struct{}

func (d *VanillaDetector) Name() string {
	return "vanilla server"
}

func (d *VanillaDetector) Detect(context DetectionContext, primaryFile *DetectionFile) (*RuntimeEvidence, error) {
	filePath := primaryFile.Path()
	zipReader, err := primaryFile.Archive()
	if err != nil {
		return nil, err
	}
	_ = context

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

			exec := &RuntimeEvidence{PrimaryPath: filePath, PrimaryRuntime: &catalog.VersionedRef{
				Ecosystem: catalog.EcosystemMinecraft,
				Name:      "minecraft",
				Version:   gameVersion,
			}, RuntimeComponents: []catalog.VersionedRef{
				{
					Ecosystem: catalog.EcosystemMinecraft,
					Name:      "minecraft",
					Version:   gameVersion,
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
