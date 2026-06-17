package detector

import (
	"archive/zip"
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
)

type geyserStandaloneDetector struct{}

func (d *geyserStandaloneDetector) Name() string {
	return "geyser standalone"
}

// Sources:
// - https://geysermc.org/wiki/geyser/setup/self/standalone
// - https://geysermc.org/wiki/geyser/setup/self/proxy-servers
// - https://geysermc.org/wiki/geyser/faq/
func (d *geyserStandaloneDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
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

	return &ExecutableEvidence{
		PrimaryEntrance: filePath,
		GameVersion:     types.VersionUnknown,
		RuntimeIdentities: []types.VersionedPackageRef{
			{
				PackageRef: types.PackageRef{
					Platform: types.PlatformAny,
					Name:     input.ToProjectName("geyser"),
				},
				Version: version,
			},
		},
		Topology: &types.RuntimeTopology{
			PrimaryNode: "geyser_standalone",
			Nodes: []types.RuntimeNode{
				{
					ID:   "geyser_standalone",
					Role: types.RuntimeRoleProxy,
					Capabilities: []types.RuntimeCapability{
						types.CapabilityProxying,
						types.CapabilityProtocolBridge,
					},
				},
			},
		},
		BridgeHints: []string{"geyser_standalone"},
	}, nil
}

type geyserStandaloneManifestSignals struct {
	mainClass string
	version   types.BareVersion
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
			signals.version = types.BareVersion(
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

func parseGeyserStandaloneVersionFromPath(filePath string) types.BareVersion {
	base := strings.ToLower(filepath.Base(filePath))
	if strings.Contains(base, "geyser") && strings.Contains(
		base,
		"standalone",
	) {
		return types.VersionUnknown
	}
	return types.VersionUnknown
}

func init() {
	registerExecutableDetector(&geyserStandaloneDetector{})
}
