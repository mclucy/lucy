package discovery

import (
	"bufio"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	versionpkg "github.com/mclucy/lucy/version"
)

const geyserStandaloneBootstrapClass = "org/geysermc/geyser/platform/standalone/GeyserStandaloneBootstrap.class"

// discoverGeyser recognizes the standalone Geyser proxy jar. The embedded
// bootstrap class is a hard gate: a manifest alone is not enough.
func discoverGeyser(s *scan) ([]Installation, []Finding) {
	var installations []Installation
	var findings []Finding
	for _, primary := range s.RootFiles() {
		if !productCandidate(s, primary) {
			continue
		}
		installation, err := inspectGeyserInstallation(s, primary)
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

// Sources:
// - https://geysermc.org/wiki/geyser/setup/self/standalone
// - https://geysermc.org/wiki/geyser/setup/self/proxy-servers
// - https://geysermc.org/wiki/geyser/faq/
func inspectGeyserInstallation(
	s *scan,
	primary *artifact,
) (*Installation, error) {
	manifest, ok, err := readProductEntry(s, primary, bukkitManifestPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	signals := parseGeyserStandaloneManifest(manifest)
	if !signals.valid() {
		return nil, nil
	}

	hasStandaloneBootstrap, err := productEntryExists(
		s,
		primary,
		geyserStandaloneBootstrapClass,
	)
	if err != nil {
		return nil, err
	}
	if !hasStandaloneBootstrap {
		return nil, nil
	}

	version := signals.version
	if !hasConcreteVersion(version) {
		version = parseGeyserStandaloneVersionFromPath(primary.Path())
	}

	identity := Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemUnspecified,
			Name:      input.ToProjectName("geyser"),
			Version:   version,
		},
	}
	installation := NewInstallation(identity, primary.Path())
	return &installation, nil
}

type geyserStandaloneManifestSignals struct {
	mainClass string
	version   versionpkg.Version
}

func (s geyserStandaloneManifestSignals) valid() bool {
	return s.mainClass == "org.geysermc.geyser.platform.standalone.GeyserStandaloneBootstrap"
}

func parseGeyserStandaloneManifest(data []byte) geyserStandaloneManifestSignals {
	var signals geyserStandaloneManifestSignals
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Main-Class: "):
			signals.mainClass = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Main-Class: ",
				),
			)
		case strings.HasPrefix(line, "Implementation-Version: "):
			signals.version = versionpkg.Version(
				strings.TrimSpace(
					strings.TrimPrefix(
						line,
						"Implementation-Version: ",
					),
				),
			)
		}
	}
	return signals
}

var geyserStandaloneVersionPattern = regexp.MustCompile(
	`geyser-standalone-(\d+\.\d+\.\d+(?:-[a-zA-Z0-9.]+)?)\.jar`,
)

func parseGeyserStandaloneVersionFromPath(filePath string) versionpkg.Version {
	match := geyserStandaloneVersionPattern.FindStringSubmatch(
		strings.ToLower(filepath.Base(filePath)),
	)
	if match == nil {
		return versionpkg.Unknown
	}
	return versionpkg.Version(match[1])
}
