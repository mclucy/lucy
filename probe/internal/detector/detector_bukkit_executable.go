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

	bukkitNodePaperFork types.RuntimeNodeID = "paper-fork"
	bukkitNodePaper     types.RuntimeNodeID = "paper"
	bukkitNodeSpigot    types.RuntimeNodeID = "spigot"
	bukkitNodeBukkit    types.RuntimeNodeID = "bukkit"
	bukkitNodeMinecraft types.RuntimeNodeID = "minecraft"
)

var bukkitVersionPrefixPattern = regexp.MustCompile(`^(\d+\.\d+(?:\.\d+)?)`)

type craftBukkitFamilyDetector struct{}

type bukkitManifestSignals struct {
	mainClass             string
	specificationTitle    string
	specificationVendor   string
	implementationTitle   string
	implementationVendor  string
	implementationVer     string
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

	judgment := newPaperJudgment()

	manifest, ok, err := readBukkitExecutableManifest(filePath, zipReader)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	signals := parseBukkitManifest(manifest)

	// Stage 1: Bukkit Confirmation
	// CraftBukkit-derived servers consistently launch through
	// org.bukkit.craftbukkit.Main, while Implementation-Title: CraftBukkit is the
	// fallback family marker seen in repackaged jars that keep the canonical
	// implementation branding. Without one of these, we should not claim a
	// Bukkit-lineage server executable.
	judgment.bukkitConfirmed = signals.mainClass == bukkitManifestMainClass ||
		strings.EqualFold(signals.implementationTitle, bukkitImplementationCraftBukkit)
	if !judgment.bukkitConfirmed {
		return nil, nil
	}
	judgment.addReason("bukkit confirmation satisfied")

	// Stage 2: Observation Extraction
	judgment.observations, err = extractPaperObservations(filePath, zipReader)
	if err != nil {
		return nil, err
	}

	// Stage 3: Family Reasoning
	reasonPaperFamily(&judgment)

	// Stage 4: Brand Attribution
	attributePaperBrand(&judgment)

	// Stage 5: Contradiction Resolution
	resolvePaperContradictions(&judgment)

	// Stage 6: Runtime Projection
	gameVersion := judgment.observations.gameVersion
	if !hasConcreteVersion(gameVersion) {
		gameVersion = types.VersionUnknown
	}
	evidence := projectPaperJudgment(filePath, gameVersion, judgment)
	if evidence == nil {
		return nil, nil
	}

	return evidence, nil
}

func readBukkitExecutableManifest(
	filePath string,
	zipReader *zip.Reader,
) ([]byte, bool, error) {
	if zipReader != nil {
		return readArchiveEntry(zipReader, bukkitManifestPath)
	}

	manifestPath := filepath.Join(filePath, filepath.FromSlash(bukkitManifestPath))
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}

	return data, true, nil
}

func reasonPaperFamily(judgment *paperJudgment) {
	if judgment == nil {
		return
	}

	switch {
	case judgment.observations.hasPaperClasses:
		judgment.familyResult = familyStrong
		judgment.addReason("paper-family confirmed by bundled paper classes")
	case judgment.observations.hasSpigotClasses:
		judgment.familyResult = familyWeak
		judgment.addReason("paper-family remains possible from spigot-lineage classes")
	default:
		judgment.familyResult = familyMiss
		judgment.addReason("paper-family classes missing after bukkit confirmation; continuing to brand attribution")
	}
}

func attributePaperBrand(judgment *paperJudgment) {
	if judgment == nil {
		return
	}

	forkBrand := normalizePaperBrandName(inferPaperObservationBrand(judgment.observations))
	officialPaper := inferOfficialPaperDistribution(judgment.observations)

	switch {
	case officialPaper && forkBrand != "" && forkBrand != "paper":
		judgment.brandResult = brandContradiction
		judgment.brandName = forkBrand
		judgment.addReason("official paper markers conflict with non-paper fork brand")
	case officialPaper:
		judgment.brandResult = brandPaper
		judgment.brandName = "paper"
		judgment.addReason("brand attributed to official paper distribution")
	case forkBrand != "":
		if forkBrand == "paper" {
			judgment.brandResult = brandPaper
			judgment.brandName = forkBrand
			judgment.addReason("brand attributed to paper")
			return
		}
		judgment.brandResult = brandFork
		judgment.brandName = forkBrand
		judgment.addReason("brand attributed to paper fork")
	default:
		judgment.brandResult = brandUnknown
		judgment.addReason("no specific paper-family brand attribution available")
	}
}

func inferOfficialPaperDistribution(obs paperObservations) bool {
	if !observationLinesContain(obs.librariesListEntries, paperLibraryPaperToken) {
		return false
	}

	return normalizePaperBrandName(inferPaperObservationBrand(obs)) == "paper"
}

func inferPaperObservationBrand(obs paperObservations) string {
	switch {
	case observationLinesContain(obs.librariesListEntries, paperLibraryFoliaToken):
		return "folia"
	case observationLinesContain(obs.librariesListEntries, paperLibraryDivineToken):
		return "divine"
	case observationLinesContain(obs.librariesListEntries, paperLibraryPurpurToken):
		return "purpur"
	case observationLinesContain(obs.librariesListEntries, paperLibraryLeafToken), obs.hasLeaperNamespace:
		return "leaf"
	case observationLinesContain(obs.librariesListEntries, paperLibraryLeavesToken), obs.hasLeavesclipNamespace, obs.leavesclipVersion != "", strings.HasPrefix(obs.buildInfo, "Leaves\t"):
		return "leaves"
	case obs.hasYouerNamespace,
		strings.EqualFold(obs.manifestSpecificationTitle, "Youer"),
		strings.EqualFold(obs.manifestImplementationTitle, "Youer"),
		strings.Contains(strings.ToLower(obs.manifestMainClass), "youer"):
		return "youer"
	case observationLinesContain(obs.librariesListEntries, paperLibraryPaperToken):
		return "paper"
	default:
		return ""
	}
}

func observationLinesContain(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

func resolvePaperContradictions(judgment *paperJudgment) {
	if judgment == nil {
		return
	}

	if judgment.familyResult == familyContradiction || judgment.brandResult == brandContradiction {
		judgment.contradictionState = true
	}
	if judgment.contradictionState {
		judgment.addReason("contradictory paper evidence resolved fail-closed to bukkit lineage")
	}
}

func projectPaperJudgment(
	filePath string,
	gameVersion types.RawVersion,
	judgment paperJudgment,
) *ExecutableEvidence {
	if !judgment.bukkitConfirmed {
		return nil
	}

	primaryNode := bukkitNodeBukkit
	brand := "bukkit"

	if judgment.contradictionState {
		judgment.addReason("runtime projection withheld paper promotion due to contradiction state")
	} else {
		switch judgment.brandResult {
		case brandPaper:
			primaryNode = bukkitNodePaper
			brand = nonEmptyPaperBrandName(judgment.brandName, "paper")
		case brandFork:
			primaryNode = bukkitNodePaperFork
			brand = nonEmptyPaperBrandName(judgment.brandName, "paper-fork")
		case brandUnknown:
			switch judgment.familyResult {
			case familyStrong:
				primaryNode = bukkitNodePaperFork
				brand = "paper-fork"
				judgment.addReason("strong paper-family evidence projected to generic paper-fork runtime")
			case familyWeak:
				primaryNode = bukkitNodeSpigot
				brand = "spigot"
				judgment.addReason("weak paper-family evidence projected to spigot runtime")
			default:
				judgment.addReason("family miss remains non-terminal but projects to baseline bukkit runtime")
			}
		}
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
			DetectorName: (&craftBukkitFamilyDetector{}).Name(),
		},
	}
}

func normalizePaperBrandName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return ""
	}

	switch normalized {
	case "craftbukkit", "bukkit":
		return ""
	default:
		return normalized
	}
}

func nonEmptyPaperBrandName(name string, fallback string) string {
	if normalized := normalizePaperBrandName(name); normalized != "" {
		return normalized
	}
	return fallback
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
		case strings.HasPrefix(line, "Specification-Title: "):
			signals.specificationTitle = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Specification-Title: ",
				),
			)
		case strings.HasPrefix(line, "Specification-Vendor: "):
			signals.specificationVendor = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Specification-Vendor: ",
				),
			)
		case strings.HasPrefix(line, "Implementation-Version: "):
			signals.implementationVer = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Implementation-Version: ",
				),
			)
		case strings.HasPrefix(line, "Implementation-Vendor: "):
			signals.implementationVendor = strings.TrimSpace(
				strings.TrimPrefix(
					line,
					"Implementation-Vendor: ",
				),
			)
		}
	}
	return signals
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
