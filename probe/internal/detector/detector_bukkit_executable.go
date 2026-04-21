package detector

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
)

const (
	bukkitManifestPath              = "META-INF/MANIFEST.MF"
	bukkitManifestMainClass         = "org.bukkit.craftbukkit.Main"
	bukkitImplementationCraftBukkit = "CraftBukkit"
	bukkitPaperClassPrefix          = "io/papermc/paper/"
	bukkitLegacyPaperClassPrefix    = "com/destroystokyo/paper/"
	bukkitSpigotClassPrefix         = "org/spigotmc/"
	bukkitBeastVersionMarker        = "beast-version.json"

	bukkitNodePaperFork types.RuntimeNodeID = "paper-fork"
	bukkitNodePaper     types.RuntimeNodeID = "paper"
	bukkitNodeSpigot    types.RuntimeNodeID = "spigot"
	bukkitNodeBukkit    types.RuntimeNodeID = "bukkit"
	bukkitNodeMinecraft types.RuntimeNodeID = "minecraft"
)

var bukkitVersionPrefixPattern = regexp.MustCompile(`^(\d+\.\d+(?:\.\d+)?)`)

type craftBukkitFamilyDetector struct{}

type bukkitManifestSignals struct {
	mainClass           string
	implementationTitle string
	implementationVer   string
}

func (d *craftBukkitFamilyDetector) Name() string {
	return "craftbukkit family executable"
}

func (d *craftBukkitFamilyDetector) Detect(
filePath string,
zipReader *zip.Reader,
fileHandle *os.File,
) (*ExecutableEvidence, error) {
	_ = fileHandle

	manifest, ok, err := readArchiveEntry(zipReader, bukkitManifestPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	signals := parseBukkitManifest(manifest)
	// CraftBukkit-derived servers consistently launch through
	// org.bukkit.craftbukkit.Main, while Implementation-Title: CraftBukkit is the
	// fallback family marker seen in repackaged jars that keep the canonical
	// implementation branding. Without one of these, we should not claim a
	// Bukkit-lineage server executable.
	if signals.mainClass != bukkitManifestMainClass &&
	!strings.EqualFold(
		signals.implementationTitle,
		bukkitImplementationCraftBukkit,
	) {
		return nil, nil
	}

	hasPaperClasses, hasSpigotClasses := classifyBukkitServerLayer(zipReader)
	brand := ""
	primaryNode := bukkitNodeBukkit
	if hasPaperClasses {
		if isOfficialPaperDistribution(filePath, signals) {
			primaryNode = bukkitNodePaper
			brand = "paper"
		} else {
			primaryNode = bukkitNodePaperFork
			brand, err = detectBukkitPaperForkBrand(zipReader, signals)
			if err != nil || brand == "" {
				brand = "paper-fork"
			}
		}
	} else if hasSpigotClasses {
		primaryNode = bukkitNodeSpigot
		brand = "spigot"
	} else {
		brand = "bukkit"
	}

	gameVersion := parseBukkitGameVersion(signals.implementationVer)
	if !hasConcreteVersion(gameVersion) {
		gameVersion = types.VersionUnknown
	}

	return &ExecutableEvidence{
		PrimaryEntrance: filePath,
		GameVersion:     gameVersion,
		RuntimeIdentities: []types.PackageId{
			{
				Platform: types.PlatformAny,
				Name:     syntax.ToProjectName(brand),
			},
			{
				Platform: types.PlatformMinecraft,
				Name:     syntax.ToProjectName("minecraft"),
				Version:  gameVersion,
			},
		},
		TopologySeed: buildBukkitExecutableTopologySeed(primaryNode),
		Provenance: ExecutableDetectorProvenance{
			DetectorName: d.Name(),
		},
	}, nil
}

func parseBukkitManifest(data []byte) bukkitManifestSignals {
	var signals bukkitManifestSignals
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
		case strings.HasPrefix(line, "Implementation-Title: "):
			signals.implementationTitle = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Implementation-Title: ",
				),
			)
		case strings.HasPrefix(line, "Implementation-Version: "):
			signals.implementationVer = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Implementation-Version: ",
				),
			)
		}
	}
	return signals
}

func classifyBukkitServerLayer(zipReader *zip.Reader) (
hasPaper bool,
hasSpigot bool,
) {
	for _, file := range zipReader.File {
		// Plugin descriptors describe what plugins can run on a server, not which
		// server implementation produced the executable jar. Class trees are the
		// durable signal because they reflect the bundled server implementation.
		switch {
		// io/papermc/paper/ and com/destroystokyo/paper/ are the Paper-specific
		// implementation packages across the modern and legacy package layouts, so
		// either prefix is enough to prove Paper-lineage internals are present.
		case strings.HasPrefix(
			file.Name,
			bukkitPaperClassPrefix,
		), strings.HasPrefix(file.Name, bukkitLegacyPaperClassPrefix):
			hasPaper = true
		case strings.HasPrefix(file.Name, bukkitSpigotClassPrefix):
			// org/spigotmc/ is Spigot-owned implementation space. It distinguishes
			// Spigot-lineage server internals from bare CraftBukkit family jars when
			// no Paper-specific packages are present.
			hasSpigot = true
		}

		if hasPaper && hasSpigot {
			break
		}
	}

	return hasPaper, hasSpigot
}

func detectBukkitPaperForkBrand(
zipReader *zip.Reader,
signals bukkitManifestSignals,
) (string, error) {
	// use implementation title
	if signals.implementationTitle != "bukkit" {
		return signals.implementationTitle, nil
	}

	// speculate from pom.xml
	for _, file := range zipReader.File {
		if !strings.HasPrefix(
			file.Name,
			"META-INF/maven/",
		) || !strings.HasSuffix(file.Name, "/pom.xml") {
			continue
		}

		r, err := file.Open()
		if err != nil {
			continue
		}

		data, readErr := io.ReadAll(r)
		_ = r.Close()
		if readErr != nil {
			continue
		}

		var pom paperForkMavenPom
		if err := xml.Unmarshal(data, &pom); err != nil {
			continue
		}

		if strings.TrimSpace(pom.Properties.MinecraftVersion) != "" {
			if artifactID := strings.TrimSpace(pom.ArtifactID); artifactID != "" {
				return artifactID, nil
			}
		}
		if strings.TrimSpace(pom.Properties.MinecraftVersionLegacy) != "" {
			if artifactID := strings.TrimSpace(pom.ArtifactID); artifactID != "" {
				return artifactID, nil
			}
		}
	}

	return "", nil
}

type paperForkMavenPom struct {
	ArtifactID string `xml:"artifactId"`
	Properties struct {
		MinecraftVersion       string `xml:"minecraft.version"`
		MinecraftVersionLegacy string `xml:"minecraft_version"`
	} `xml:"properties"`
}

func isOfficialPaperDistribution(
filePath string,
signals bukkitManifestSignals,
) bool {
	title := strings.ToLower(signals.implementationTitle)
	base := strings.ToLower(filepath.Base(filePath))
	return strings.Contains(title, "paper") || strings.Contains(base, "paper")
}

func parseBukkitGameVersion(implementationVersion string) types.RawVersion {
	match := bukkitVersionPrefixPattern.FindStringSubmatch(strings.TrimSpace(implementationVersion))
	if len(match) < 2 || !isMinecraftReleaseVersion(match[1]) {
		return types.VersionUnknown
	}
	return types.RawVersion(match[1])
}

func buildBukkitExecutableTopologySeed(
primaryNode types.RuntimeNodeID,
) *ExecutableTopologySeed {
	nodes := []types.RuntimeNode{}
	edges := []types.RuntimeEdge{}

	addNode := func(id types.RuntimeNodeID) {
		nodes = append(nodes, buildBukkitExecutableNode(id))
	}

	switch primaryNode {
	case bukkitNodePaperFork:
		addNode(bukkitNodePaperFork)
		addNode(bukkitNodePaper)
		addNode(bukkitNodeMinecraft)
		edges = append(
			edges,
			buildBukkitImplementationEdge(
				bukkitNodePaperFork,
				bukkitNodePaper,
				types.EdgeImplements,
			),
			buildBukkitImplementationEdge(
				bukkitNodePaper,
				bukkitNodeMinecraft,
				types.EdgeModifies,
			),
		)
	case bukkitNodePaper:
		addNode(bukkitNodePaper)
		addNode(bukkitNodeMinecraft)
		edges = append(
			edges,
			buildBukkitImplementationEdge(
				bukkitNodePaper,
				bukkitNodeMinecraft,
				types.EdgeModifies,
			),
		)
	case bukkitNodeSpigot:
		addNode(bukkitNodeSpigot)
		addNode(bukkitNodeMinecraft)
		edges = append(
			edges,
			buildBukkitImplementationEdge(
				bukkitNodeSpigot,
				bukkitNodeMinecraft,
				types.EdgeModifies,
			),
		)
	default:
		addNode(bukkitNodeBukkit)
	}

	return &ExecutableTopologySeed{
		PrimaryNode: primaryNode,
		Nodes:       nodes,
		Edges:       edges,
	}
}

func buildBukkitExecutableNode(id types.RuntimeNodeID) types.RuntimeNode {
	return types.RuntimeNode{
		ID:           id,
		Role:         types.RuntimeRolePluginCore,
		Capabilities: []types.RuntimeCapability{types.CapabilityBukkitPlugins},
	}
}

func buildBukkitImplementationEdge(
from types.RuntimeNodeID,
to types.RuntimeNodeID,
verb types.RuntimeEdgeVerb,
) types.RuntimeEdge {
	return types.RuntimeEdge{
		From: from,
		To:   to,
		Verb: verb,
	}
}

func init() {
	registerExecutableDetector(&craftBukkitFamilyDetector{})
}
