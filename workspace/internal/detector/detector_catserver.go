package detector

import (
	"archive/zip"
	"bufio"
	"os"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
)

var catServerVersionPattern = regexp.MustCompile(
	`^(?:git-CatServer-)?(\d+\.\d+(?:\.\d+)?)-`,
)

type catServerDetector struct{}

func (d *catServerDetector) Name() string {
	return "catserver executable"
}

func (d *catServerDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	_ = fileHandle

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

	return &ExecutableEvidence{PrimaryPath: filePath, PrimaryRuntime: &types.VersionedPackageRef{
		Eco:     types.EcoUnspecified,
		Name:    input.ToProjectName("catserver"),
		Version: signals.version,
	}, RuntimeComponents: []types.VersionedPackageRef{
		{
			Eco:     types.EcoMinecraft,
			Name:    input.ToProjectName("minecraft"),
			Version: signals.gameVersion,
		},
	}}, nil
}

type catServerManifestSignals struct {
	mainClass   string
	title       string
	version     types.BareVersion
	gameVersion types.BareVersion
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
			signals.version = types.BareVersion(version)
			signals.gameVersion = parseCatServerGameVersion(version)
		}
	}
	return signals
}

func parseCatServerGameVersion(version string) types.BareVersion {
	match := catServerVersionPattern.FindStringSubmatch(version)
	if match == nil {
		return types.VersionUnknown
	}
	return types.BareVersion(match[1])
}

func init() {
	registerExecutableDetector(&catServerDetector{})
}
