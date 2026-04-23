package detector

import (
	"archive/zip"
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	metaMainClass, err := readBukkitExecutableSidecar(filePath, zipReader, paperMetaMainClassPath)
	if err != nil {
		return nil, err
	}
	reaperPatchProperties, err := readBukkitExecutablePatchProperties(filePath, zipReader)
	if err != nil {
		return nil, err
	}

	// Stage 1: Bukkit Confirmation
	// CraftBukkit-derived servers consistently launch through
	// org.bukkit.craftbukkit.Main, while Implementation-Title: CraftBukkit is the
	// fallback family marker seen in repackaged jars that keep the canonical
	// implementation branding. Extracted modern Paper fixtures keep the decisive
	// Bukkit entrypoint in META-INF/main-class, while strict launcher-heavy Reaper
	// and Youer fixtures only expose definitive Paper-fork proof via patch or
	// manifest identity. Without one of these strict signals, we should not claim
	// a Bukkit-lineage server executable.
	judgment.bukkitConfirmed = signals.mainClass == bukkitManifestMainClass ||
		strings.EqualFold(signals.implementationTitle, bukkitImplementationCraftBukkit) ||
		metaMainClass == bukkitManifestMainClass ||
		hasStrictReaperBukkitConfirmation(reaperPatchProperties) ||
		hasStrictYouerBukkitConfirmation(signals)
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
	case hasModernPaperclipMetadataCluster(judgment.observations):
		judgment.familyResult = familyStrong
		judgment.addReason("paper-family confirmed by modern paperclip metadata cluster")
	case hasConcreteVersion(judgment.observations.versionJSONID):
		judgment.familyResult = familyWeak
		judgment.addReason("paper-family remains likely from root version.json metadata")
	case len(judgment.observations.patchProperties) > 0 || judgment.observations.hasPaperMCPatch:
		judgment.familyResult = familyWeak
		judgment.addReason("paper-family remains likely from legacy paper patch traces")
	case judgment.observations.hasPaperclipNamespace || judgment.observations.hasLegacyPaperclipNamespace:
		judgment.familyResult = familyWeak
		judgment.addReason("paper-family remains likely from paperclip launcher namespaces")
	default:
		judgment.familyResult = familyMiss
		judgment.addReason("paper-family evidence missing after bukkit confirmation; continuing to brand attribution")
	}
}

func hasModernPaperclipMetadataCluster(obs paperObservations) bool {
	return strings.TrimSpace(obs.downloadContext) != "" &&
		len(obs.librariesListEntries) > 0 &&
		strings.TrimSpace(obs.metaMainClass) != ""
}

func attributePaperBrand(judgment *paperJudgment) {
	if judgment == nil {
		return
	}

	brands := inferPaperObservationBrands(judgment.observations)

	switch {
	case len(brands) > 1:
		judgment.brandResult = brandContradiction
		judgment.brandName = strings.Join(brands, ",")
		judgment.addReason(fmt.Sprintf("contradictory paper brands detected: %s", strings.Join(brands, ", ")))
	case len(brands) == 1 && brands[0] == "paper":
		judgment.brandResult = brandPaper
		judgment.brandName = "paper"
		judgment.addReason("brand attributed to official paper distribution")
	case len(brands) == 1:
		judgment.brandResult = brandFork
		judgment.brandName = brands[0]
		judgment.addReason("brand attributed to paper fork")
	default:
		judgment.brandResult = brandUnknown
		judgment.addReason("no specific paper-family brand attribution available")
	}
}

func inferPaperObservationBrands(obs paperObservations) []string {
	brands := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	add := func(name string) {
		normalized := normalizePaperBrandName(name)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		brands = append(brands, normalized)
	}

	// Fixture citation: probe/internal/detector/testdata/paper_family/test_paper/paper/META-INF/libraries.list
	if observationLinesContain(obs.librariesListEntries, paperLibraryPaperToken) {
		add("paper")
	}
	// Fixture citation: probe/internal/detector/testdata/paper_family/test_folia/folia/META-INF/libraries.list
	if observationLinesContain(obs.librariesListEntries, paperLibraryFoliaToken) {
		add("folia")
	}
	// Fixture citation: probe/internal/detector/testdata/paper_family/test_divine/divine/META-INF/libraries.list
	if observationLinesContain(obs.librariesListEntries, paperLibraryDivineToken) {
		add("divine")
	}
	// Fixture citation: probe/internal/detector/testdata/paper_family/test_purpur/purpur/META-INF/libraries.list
	if observationLinesContain(obs.librariesListEntries, paperLibraryPurpurToken) {
		add("purpur")
	}
	// Fixture citation: probe/internal/detector/testdata/paper_family/test_leaf/leaf/META-INF/libraries.list and cn/dreeam/leaper/*
	if observationLinesContain(obs.librariesListEntries, paperLibraryLeafToken) || obs.hasLeaperNamespace {
		add("leaf")
	}
	// Fixture citation: probe/internal/detector/testdata/paper_family/test_leaves/leaves/META-INF/libraries.list, META-INF/build-info, META-INF/leavesclip-version
	if observationLinesContain(obs.librariesListEntries, paperLibraryLeavesToken) || obs.hasLeavesclipNamespace || obs.leavesclipVersion != "" || strings.HasPrefix(obs.buildInfo, "Leaves\t") {
		add("leaves")
	}
	// Fixture citation: probe/internal/detector/testdata/paper_family/test_reaper/reaper/patch.properties
	if hasStrictReaperObservationBrand(obs) {
		add("reaper")
	}
	// Fixture citation: probe/internal/detector/testdata/paper_family/test_youer/youer/META-INF/MANIFEST.MF
	if obs.hasYouerNamespace ||
		strings.EqualFold(obs.manifestSpecificationTitle, paperManifestYouerToken) ||
		strings.EqualFold(obs.manifestImplementationTitle, paperManifestYouerToken) ||
		strings.Contains(strings.ToLower(obs.manifestMainClass), paperMainClassYouerToken) {
		add("youer")
	}

	slices.Sort(brands)
	return brands
}

func readBukkitExecutableSidecar(
	filePath string,
	zipReader *zip.Reader,
	entryPath string,
) (string, error) {
	var (
		data []byte
		ok   bool
		err  error
	)

	if zipReader != nil {
		data, ok, err = readArchiveEntry(zipReader, entryPath)
	} else {
		data, ok, err = readDirectoryEntry(filePath, entryPath)
	}
	if err != nil || !ok {
		return "", err
	}

	return strings.TrimSpace(string(data)), nil
}

func readBukkitExecutablePatchProperties(
	filePath string,
	zipReader *zip.Reader,
) (map[string]string, error) {
	var (
		data []byte
		ok   bool
		err  error
	)

	if zipReader != nil {
		data, ok, err = readArchiveEntry(zipReader, paperPatchPropertiesPath)
	} else {
		data, ok, err = readDirectoryEntry(filePath, paperPatchPropertiesPath)
	}
	if err != nil || !ok {
		return nil, err
	}

	return parsePaperPatchProperties(data), nil
}

func readDirectoryEntry(root string, entryPath string) ([]byte, bool, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entryPath)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func hasStrictReaperBukkitConfirmation(properties map[string]string) bool {
	return strings.Contains(properties["patch"], paperPatchReaperToken)
}

func hasStrictYouerBukkitConfirmation(signals bukkitManifestSignals) bool {
	return strings.EqualFold(signals.specificationTitle, paperManifestYouerToken) ||
		strings.EqualFold(signals.implementationTitle, paperManifestYouerToken) ||
		strings.Contains(strings.ToLower(signals.mainClass), paperMainClassYouerToken)
}

func hasStrictReaperObservationBrand(obs paperObservations) bool {
	return strings.Contains(obs.patchProperties["patch"], paperPatchReaperToken)
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

	if judgment.brandResult == brandContradiction {
		judgment.familyResult = familyContradiction
		judgment.contradictionState = fmt.Sprintf(
			"brand contradiction after bukkit confirmation: %s",
			nonEmptyPaperBrandName(judgment.brandName, "unknown"),
		)
	}
	if judgment.familyResult == familyContradiction && judgment.contradictionState == "" {
		judgment.contradictionState = "paper family contradiction after bukkit confirmation"
	}
	if judgment.contradictionState != "" {
		judgment.addReason(judgment.contradictionState)
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

	if judgment.contradictionState != "" {
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
	case brandContradiction:
		// contradictionState is set by resolvePaperContradictions; projection
		// already withheld above via the contradictionState guard. This arm
		// makes the switch exhaustive over all paperBrandResult values.
		judgment.addReason("brand contradiction: projection falls back to bukkit baseline")
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
