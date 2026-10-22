package detector

import (
	"bufio"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	versionpkg "github.com/mclucy/lucy/version"
)

type geyserStandaloneDetector struct{}

func (d *geyserStandaloneDetector) Name() string {
	return "geyser standalone"
}

// Sources:
// - https://geysermc.org/wiki/geyser/setup/self/standalone
// - https://geysermc.org/wiki/geyser/setup/self/proxy-servers
// - https://geysermc.org/wiki/geyser/faq/
func (d *geyserStandaloneDetector) Detect(context DetectionContext, primaryFile *DetectionFile) (*RuntimeEvidence, error) {
	filePath := primaryFile.Path()
	zipReader, err := primaryFile.Archive()
	if err != nil {
		return nil, err
	}
	_ = context

	manifest, ok, err := readArchiveEntry(zipReader, "META-INF/MANIFEST.MF")
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

	hasStandaloneBootstrap, err := archiveContains(
		zipReader,
		"org/geysermc/geyser/platform/standalone/GeyserStandaloneBootstrap.class",
	)
	if err != nil {
		return nil, err
	}
	if !hasStandaloneBootstrap {
		return nil, nil
	}

	version := signals.version
	if !hasConcreteVersion(version) {
		version = parseGeyserStandaloneVersionFromPath(filePath)
	}

	return &RuntimeEvidence{PrimaryPath: filePath, PrimaryRuntime: &catalog.VersionedRef{
		Ecosystem: catalog.EcosystemUnspecified,
		Name:      input.ToProjectName("geyser"),
		Version:   version,
	}, RuntimeComponents: nil}, nil
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

func init() {
	registerExecutableDetector(&geyserStandaloneDetector{})
}
