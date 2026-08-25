package detector

import (
	"archive/zip"
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
)

var (
	spongeVanillaVersionPattern = regexp.MustCompile(
		`^(\d+\.\d+(?:\.\d+)?)-(\d+(?:\.\d+)+)$`,
	)
	spongeHybridVersionPattern = regexp.MustCompile(
		`^(\d+\.\d+(?:\.\d+)?)-(\d+(?:\.\d+)+)-(\d+(?:\.\d+)+)$`,
	)
	spongeUniversalJarPattern = regexp.MustCompile(
		`(?i)^sponge(?:vanilla|forge|neo)-.+\.jar$`,
	)
)

type spongeFlavor int

const (
	spongeFlavorUnknown spongeFlavor = iota
	spongeFlavorVanilla
	spongeFlavorForge
	spongeFlavorNeo
)

type spongeServerDetector struct{}

func (d *spongeServerDetector) Name() string {
	return "sponge server"
}

func (d *spongeServerDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	_ = fileHandle

	if !spongeUniversalJarPattern.MatchString(strings.ToLower(filepath.Base(filePath))) {
		return nil, nil
	}

	manifest, ok, err := readArchiveEntry(zipReader, "META-INF/MANIFEST.MF")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	signals := parseSpongeManifest(manifest)
	if !signals.valid() {
		return nil, nil
	}

	return buildSpongeExecutableEvidence(filePath, signals), nil
}

type spongeManifestSignals struct {
	title         string
	vendor        string
	version       string
	flavor        spongeFlavor
	gameVersion   types.BareVersion
	loaderVersion types.BareVersion
	spongeVersion types.BareVersion
}

func (s spongeManifestSignals) valid() bool {
	if !strings.EqualFold(strings.TrimSpace(s.vendor), "SpongePowered") {
		return false
	}
	if s.flavor == spongeFlavorUnknown {
		return false
	}
	if !hasConcreteVersion(s.gameVersion) {
		return false
	}
	if !hasConcreteVersion(s.spongeVersion) {
		return false
	}
	if s.flavor != spongeFlavorVanilla && !hasConcreteVersion(s.loaderVersion) {
		return false
	}

	return true
}

func parseSpongeManifest(data []byte) spongeManifestSignals {
	var signals spongeManifestSignals
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inNamedSection := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Name: ") {
			inNamedSection = true
			continue
		}
		if strings.TrimSpace(line) == "" {
			inNamedSection = false
			continue
		}
		if inNamedSection {
			continue
		}

		switch {
		case strings.HasPrefix(line, "Specification-Title: "):
			signals.title = strings.TrimSpace(
				strings.TrimPrefix(line, "Specification-Title: "),
			)
		case strings.HasPrefix(line, "Specification-Vendor: "):
			if signals.vendor == "" {
				signals.vendor = strings.TrimSpace(
					strings.TrimPrefix(line, "Specification-Vendor: "),
				)
			}
		case strings.HasPrefix(line, "Implementation-Title: "):
			if signals.title == "" {
				signals.title = strings.TrimSpace(
					strings.TrimPrefix(line, "Implementation-Title: "),
				)
			}
		case strings.HasPrefix(line, "Implementation-Vendor: "):
			if signals.vendor == "" {
				signals.vendor = strings.TrimSpace(
					strings.TrimPrefix(line, "Implementation-Vendor: "),
				)
			}
		case strings.HasPrefix(line, "Implementation-Version: "):
			if signals.version == "" {
				signals.version = strings.TrimSpace(
					strings.TrimPrefix(line, "Implementation-Version: "),
				)
			}
		}
	}

	signals.flavor, signals.gameVersion, signals.loaderVersion, signals.spongeVersion = parseSpongeImplementationVersion(
		signals.title,
		signals.version,
	)

	return signals
}

func parseSpongeImplementationVersion(
	title string,
	version string,
) (
	flavor spongeFlavor,
	gameVersion types.BareVersion,
	loaderVersion types.BareVersion,
	spongeVersion types.BareVersion,
) {
	version = strings.TrimSpace(version)
	switch strings.TrimSpace(title) {
	case "SpongeVanilla":
		match := spongeVanillaVersionPattern.FindStringSubmatch(version)
		if match == nil {
			return spongeFlavorUnknown, types.VersionUnknown, types.VersionUnknown, types.VersionUnknown
		}
		return spongeFlavorVanilla,
			types.BareVersion(match[1]),
			types.VersionUnknown,
			types.BareVersion(match[2])
	case "SpongeForge":
		match := spongeHybridVersionPattern.FindStringSubmatch(version)
		if match == nil {
			return spongeFlavorUnknown, types.VersionUnknown, types.VersionUnknown, types.VersionUnknown
		}
		return spongeFlavorForge,
			types.BareVersion(match[1]),
			types.BareVersion(match[2]),
			types.BareVersion(match[3])
	case "SpongeNeo":
		match := spongeHybridVersionPattern.FindStringSubmatch(version)
		if match == nil {
			return spongeFlavorUnknown, types.VersionUnknown, types.VersionUnknown, types.VersionUnknown
		}
		return spongeFlavorNeo,
			types.BareVersion(match[1]),
			types.BareVersion(match[2]),
			types.BareVersion(match[3])
	default:
		return spongeFlavorUnknown, types.VersionUnknown, types.VersionUnknown, types.VersionUnknown
	}
}

func buildSpongeExecutableEvidence(
	filePath string,
	signals spongeManifestSignals,
) *ExecutableEvidence {
	primaryName := "spongevanilla"
	switch signals.flavor {
	case spongeFlavorForge:
		primaryName = "spongeforge"
	case spongeFlavorNeo:
		primaryName = "spongeneo"
	}
	primary := types.VersionedPackageRef{
		Eco:     types.EcoSponge,
		Name:    input.ToProjectName(primaryName),
		Version: signals.spongeVersion,
	}
	components := []types.VersionedPackageRef{
		{
			Eco:     types.EcoMinecraft,
			Name:    input.ToProjectName("minecraft"),
			Version: signals.gameVersion,
		},
	}

	switch signals.flavor {
	case spongeFlavorForge:
		components = append(
			components, types.VersionedPackageRef{
				Eco:     types.EcoForge,
				Name:    input.ToProjectName("forge"),
				Version: signals.loaderVersion,
			},
		)
	case spongeFlavorNeo:
		components = append(
			components, types.VersionedPackageRef{
				Eco:     types.EcoNeoforge,
				Name:    input.ToProjectName("neoforge"),
				Version: signals.loaderVersion,
			},
		)
	}

	return &ExecutableEvidence{
		PrimaryPath:       filePath,
		PrimaryRuntime:    &primary,
		RuntimeComponents: components,
	}
}

func init() {
	registerExecutableDetector(&spongeServerDetector{})
}
