package detector

import (
	"archive/zip"
	"bufio"
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

	bukkitNodePaperFork   types.RuntimeNodeID = "paper-fork"
	bukkitNodePaper       types.RuntimeNodeID = "paper"
	bukkitNodeSpigot      types.RuntimeNodeID = "spigot"
	bukkitNodeCraftBukkit types.RuntimeNodeID = "craftbukkit"
	bukkitNodeBukkit      types.RuntimeNodeID = "bukkit"
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
		!strings.EqualFold(signals.implementationTitle, bukkitImplementationCraftBukkit) {
		return nil, nil
	}

	hasPaperClasses, hasSpigotClasses := classifyBukkitServerLayer(zipReader)
	brand := ""
	primaryNode := bukkitNodeBukkit
	if hasPaperClasses {
		brand, err = detectBukkitPaperForkBrand(filePath, zipReader, signals)
		if err != nil {
			return nil, err
		}
		if brand != "" {
			primaryNode = bukkitNodePaperFork
		} else if isOfficialPaperDistribution(filePath, signals) {
			primaryNode = bukkitNodePaper
			brand = "paper"
		} else {
			primaryNode = bukkitNodePaperFork
			brand = "paper-fork"
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
			signals.mainClass = strings.TrimSpace(strings.TrimPrefix(line, "Main-Class: "))
		case strings.HasPrefix(line, "Implementation-Title: "):
			signals.implementationTitle = strings.TrimSpace(strings.TrimPrefix(line, "Implementation-Title: "))
		case strings.HasPrefix(line, "Implementation-Version: "):
			signals.implementationVer = strings.TrimSpace(strings.TrimPrefix(line, "Implementation-Version: "))
		}
	}
	return signals
}

func classifyBukkitServerLayer(zipReader *zip.Reader) (hasPaper bool, hasSpigot bool) {
	for _, file := range zipReader.File {
		// Plugin descriptors describe what plugins can run on a server, not which
		// server implementation produced the executable jar. Class trees are the
		// durable signal because they reflect the bundled server implementation.
		switch {
		// io/papermc/paper/ and com/destroystokyo/paper/ are the Paper-specific
		// implementation packages across the modern and legacy package layouts, so
		// either prefix is enough to prove Paper-lineage internals are present.
		case strings.HasPrefix(file.Name, bukkitPaperClassPrefix), strings.HasPrefix(file.Name, bukkitLegacyPaperClassPrefix):
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
	filePath string,
	zipReader *zip.Reader,
	signals bukkitManifestSignals,
) (string, error) {
	// Public Paper forks are often repackaged with upstream CraftBukkit metadata,
	// so fork-brand identification must stay best-effort. If the fork cannot be
	// proven from explicit markers, we fall back to the generic paper-fork brand.
	if strings.Contains(strings.ToLower(signals.implementationTitle), "beast") {
		return "beast", nil
	}

	hasBeastVersion, err := archiveContains(zipReader, bukkitBeastVersionMarker)
	if err != nil {
		return "", err
	}
	if hasBeastVersion || strings.Contains(strings.ToLower(filepath.Base(filePath)), "beast") {
		return "beast", nil
	}

	for _, file := range zipReader.File {
		base := strings.ToLower(filepath.Base(file.Name))
		if strings.Contains(base, "beast") && strings.Contains(base, "version") {
			return "beast", nil
		}
	}

	return "", nil
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
		addNode(bukkitNodeSpigot)
		addNode(bukkitNodeCraftBukkit)
		edges = append(edges,
			buildBukkitLineageEdge(bukkitNodePaperFork, bukkitNodePaper),
			buildBukkitLineageEdge(bukkitNodePaper, bukkitNodeSpigot),
			buildBukkitLineageEdge(bukkitNodeSpigot, bukkitNodeCraftBukkit),
		)
	case bukkitNodePaper:
		addNode(bukkitNodePaper)
		addNode(bukkitNodeSpigot)
		addNode(bukkitNodeCraftBukkit)
		edges = append(edges,
			buildBukkitLineageEdge(bukkitNodePaper, bukkitNodeSpigot),
			buildBukkitLineageEdge(bukkitNodeSpigot, bukkitNodeCraftBukkit),
		)
	case bukkitNodeSpigot:
		addNode(bukkitNodeSpigot)
		addNode(bukkitNodeCraftBukkit)
		edges = append(edges,
			buildBukkitLineageEdge(bukkitNodeSpigot, bukkitNodeCraftBukkit),
		)
	default:
		// The lineage chain bottoms out at CraftBukkit because that is the concrete
		// implementation layer. Bukkit itself is an API/spec identity, so the bare
		// fallback tier reports a single bukkit node instead of pretending there is
		// another implementation edge below it.
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
		ID:               id,
		Role:             types.RuntimeRolePluginCore,
		IdentityPlatform: types.PlatformAny,
		Capabilities:     []types.RuntimeCapability{types.CapabilityBukkitPlugins},
	}
}

func buildBukkitLineageEdge(from types.RuntimeNodeID, to types.RuntimeNodeID) types.RuntimeEdge {
	return types.RuntimeEdge{
		From: from,
		To:   to,
		// The topology model does not have a dedicated extends edge yet. EdgeAdapts
		// is the closest existing relation for "this runtime layer is built on top of
		// that implementation layer" until the shared edge vocabulary grows.
		Kind: types.EdgeAdapts,
		Risk: types.RiskNone,
	}
}

func init() {
	registerExecutableDetector(&craftBukkitFamilyDetector{})
}
