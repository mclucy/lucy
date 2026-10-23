package discovery

import (
	"bufio"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	versionpkg "github.com/mclucy/lucy/version"
)

var catServerVersionPattern = regexp.MustCompile(
	`^(?:git-CatServer-)?(\d+\.\d+(?:\.\d+)?)-`,
)

type catServerDetector struct{}

func (d *catServerDetector) Name() string {
	return "catserver executable"
}

func (d *catServerDetector) Detect(context DetectionContext, primaryFile *DetectionFile) (*RuntimeEvidence, error) {
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

	signals := parseCatServerManifest(manifest)
	if !signals.valid() {
		return nil, nil
	}

	return &RuntimeEvidence{PrimaryPath: filePath, PrimaryRuntime: &catalog.VersionedRef{
		Ecosystem: catalog.EcosystemUnspecified,
		Name:      input.ToProjectName("catserver"),
		Version:   signals.version,
	}, RuntimeComponents: []catalog.VersionedRef{
		{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      input.ToProjectName("minecraft"),
			Version:   signals.gameVersion,
		},
	}}, nil
}

type catServerManifestSignals struct {
	mainClass   string
	title       string
	version     versionpkg.Version
	gameVersion versionpkg.Version
}

func (s catServerManifestSignals) valid() bool {
	return s.title == "CatServer" &&
		s.mainClass == "catserver.server.CatServerLaunch" &&
		hasConcreteVersion(s.gameVersion)
}

func parseCatServerManifest(data []byte) catServerManifestSignals {
	var signals catServerManifestSignals
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Main-Class: "):
			signals.mainClass = strings.TrimSpace(
				strings.TrimPrefix(line, "Main-Class: "),
			)
		case strings.HasPrefix(line, "Implementation-Title: "):
			signals.title = strings.TrimSpace(
				strings.TrimPrefix(line, "Implementation-Title: "),
			)
		case strings.HasPrefix(line, "Implementation-Version: "):
			version := strings.TrimSpace(
				strings.TrimPrefix(line, "Implementation-Version: "),
			)
			signals.version = versionpkg.Version(version)
			signals.gameVersion = parseCatServerGameVersion(version)
		}
	}
	return signals
}

func parseCatServerGameVersion(version string) versionpkg.Version {
	match := catServerVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return versionpkg.Unknown
	}
	return versionpkg.Version(match[1])
}

func init() {
	registerExecutableDetector(&catServerDetector{})
}
