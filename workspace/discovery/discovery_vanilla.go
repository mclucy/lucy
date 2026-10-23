package discovery

import (
	"github.com/mclucy/lucy/catalog"
)

// discoverVanilla recognizes Mojang server jars. A Fabric meta launcher also
// embeds version.json; it is rejected here because the loader installation
// owns that artifact, not plain Minecraft.
func discoverVanilla(s *scan) ([]Installation, []Finding) {
	var installations []Installation
	var findings []Finding
	for _, primary := range s.RootFiles() {
		if !productCandidate(s, primary) {
			continue
		}
		installation, err := inspectVanillaInstallation(s, primary)
		if err != nil {
			findings = append(findings, Finding{
				Kind:  FindingUnreadableFile,
				Path:  primary.Path(),
				Cause: err,
			})
			continue
		}
		if installation != nil {
			installations = append(installations, *installation)
		}
	}
	return installations, findings
}

func inspectVanillaInstallation(
	s *scan,
	primary *artifact,
) (*Installation, error) {
	launchProperties, hasLaunchProperties, err := readProductEntry(
		s,
		primary,
		fabricLaunchPropertiesPath,
	)
	if err != nil {
		return nil, err
	}
	if hasLaunchProperties && fabricLauncherPropertiesAreServer(launchProperties) {
		return nil, nil
	}

	versionJSON, ok, err := readProductEntry(s, primary, mojangVersionJSONEntry)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	gameVersion, guarded, err := mojangVersionFromJSON(versionJSON)
	if err != nil {
		return nil, err
	}
	if guarded {
		return nil, nil
	}

	identity := Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      catalog.Name("minecraft"),
			Version:   observedVersion(gameVersion),
		},
	}
	installation := NewInstallation(identity, primary.Path())
	return &installation, nil
}
