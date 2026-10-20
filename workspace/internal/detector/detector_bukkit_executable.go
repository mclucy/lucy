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

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
)

const (
	bukkitManifestPath              = "META-INF/MANIFEST.MF"
	bukkitManifestMainClass         = "org.bukkit.craftbukkit.Main"
	bukkitImplementationCraftBukkit = "CraftBukkit"
	bukkitPaperClassPrefix          = "io/papermc/paper/"
	bukkitLegacyPaperClassPrefix    = "com/destroystokyo/paper/"
	bukkitSpigotClassPrefix         = "org/spigotmc/"
)

var bukkitVersionPrefixPattern = regexp.MustCompile(`^(\d+\.\d+(?:\.\d+)?)`)

// paperLineages must cover every lineage inferPaperObservationLineages can
// attribute; the fork-lineage fixture test iterates this map.
var paperLineages = map[string]bool{
	"paper":  true,
	"folia":  true,
	"divine": true,
	"purpur": true,
	"leaf":   true,
	"leaves": true,
	"reaper": true,
	"youer":  true,
}

// IsPaperLineage reports whether name is a paper-lineage runtime identity
// this package can emit as a primary runtime identity. Youer is included
// even though it is a NeoForge hybrid; treat it separately before calling.
func IsPaperLineage(name string) bool {
	return paperLineages[name]
}

type craftBukkitFamilyDetector struct{}

type bukkitManifestSignals struct {
	mainClass            string
	specificationTitle   string
	specificationVendor  string
	implementationTitle  string
	implementationVendor string
	implementationVer    string
}

func (d *craftBukkitFamilyDetector) Name() string {
	return "craftbukkit family executable"
}

func (d *craftBukkitFamilyDetector) Detect(context DetectionContext, primaryFile *DetectionFile) (*RuntimeEvidence, error) {
	filePath := primaryFile.Path()
	zipReader, err := primaryFile.Archive()
	if err != nil {
		return nil, err
	}
	_ = context

	judgment := newPaperJudgment()

	manifest, ok, err := readBukkitExecutableManifest(filePath, zipReader)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	signals := parseBukkitManifest(manifest)
	metaMainClass, err := readBukkitExecutableSidecar(
		filePath,
		zipReader,
		paperMetaMainClassPath,
	)
	if err != nil {
		return nil, err
	}
	reaperPatchProperties, err := readBukkitExecutablePatchProperties(
		filePath,
		zipReader,
	)
	if err != nil {
		return nil, err
	}

	// Stage 1: Bukkit Confirmation
	// CraftBukkit-derived servers consistently launch through
	// org.bukkit.craftbukkit.Main, while Implementation-Title: CraftBukkit is the
	// fallback family marker seen in repackaged jars that keep the canonical
	// implementation title. Extracted modern Paper fixtures keep the decisive
	// Bukkit entrypoint in META-INF/main-class, while strict launcher-heavy Reaper
	// and Youer fixtures only expose definitive Paper-fork proof via patch or
	// manifest identity. Without one of these strict signals, we should not claim
	// a Bukkit-lineage server executable.
	judgment.bukkitConfirmed = signals.mainClass == bukkitManifestMainClass ||
		strings.EqualFold(
			signals.implementationTitle,
			bukkitImplementationCraftBukkit,
		) ||
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

	// Stage 4: Lineage Attribution
	attributePaperLineage(&judgment)

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

	manifestPath := filepath.Join(
		filePath,
		filepath.FromSlash(bukkitManifestPath),
	)
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
		judgment.addReason("paper-family evidence missing after bukkit confirmation; continuing to lineage attribution")
	}
}

func hasModernPaperclipMetadataCluster(obs paperObservations) bool {
	return strings.TrimSpace(obs.downloadContext) != "" &&
		len(obs.librariesListEntries) > 0 &&
		strings.TrimSpace(obs.metaMainClass) != ""
}

func attributePaperLineage(judgment *paperJudgment) {
	if judgment == nil {
		return
	}

	lineages := inferPaperObservationLineages(judgment.observations)

	switch {
	case len(lineages) > 1:
		judgment.lineageResult = lineageContradiction
		judgment.lineageName = strings.Join(lineages, ",")
		judgment.addReason(
			fmt.Sprintf(
				"contradictory paper lineages detected: %s",
				strings.Join(lineages, ", "),
			),
		)
	case len(lineages) == 1 && lineages[0] == "paper":
		judgment.lineageResult = lineagePaper
		judgment.lineageName = "paper"
		judgment.addReason("lineage attributed to official paper distribution")
	case len(lineages) == 1:
		judgment.lineageResult = lineageFork
		judgment.lineageName = lineages[0]
		judgment.addReason("lineage attributed to paper fork")
	default:
		judgment.lineageResult = lineageUnknown
		judgment.addReason("no specific paper-family lineage attribution available")
	}
}

func inferPaperObservationLineages(obs paperObservations) []string {
	lineages := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	add := func(name string) {
		normalized := normalizePaperLineageName(name)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		lineages = append(lineages, normalized)
	}

	// Fixture citation: workspace/internal/detector/testdata/paper_family/test_paper/paper/META-INF/libraries.list
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryPaperToken,
	) {
		add("paper")
	}
	// Fixture citation: workspace/internal/detector/testdata/paper_family/test_folia/folia/META-INF/libraries.list
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryFoliaToken,
	) {
		add("folia")
	}
	// Fixture citation: workspace/internal/detector/testdata/paper_family/test_divine/divine/META-INF/libraries.list
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryDivineToken,
	) {
		add("divine")
	}
	// Fixture citation: workspace/internal/detector/testdata/paper_family/test_purpur/purpur/META-INF/libraries.list
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryPurpurToken,
	) {
		add("purpur")
	}
	// Fixture citation: workspace/internal/detector/testdata/paper_family/test_leaf/leaf/META-INF/libraries.list and cn/dreeam/leaper/*
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryLeafToken,
	) || obs.hasLeaperNamespace {
		add("leaf")
	}
	// Fixture citation: workspace/internal/detector/testdata/paper_family/test_leaves/leaves/META-INF/libraries.list, META-INF/build-info, META-INF/leavesclip-version
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryLeavesToken,
	) || obs.hasLeavesclipNamespace || obs.leavesclipVersion != "" || strings.HasPrefix(
		obs.buildInfo,
		"Leaves\t",
	) {
		add("leaves")
	}
	// Fixture citation: workspace/internal/detector/testdata/paper_family/test_reaper/reaper/patch.properties
	if hasStrictReaperObservationLineage(obs) {
		add("reaper")
	}
	// Fixture citation: workspace/internal/detector/testdata/paper_family/test_youer/youer/META-INF/MANIFEST.MF
	if obs.hasYouerNamespace ||
		strings.EqualFold(
			obs.manifestSpecificationTitle,
			paperManifestYouerToken,
		) ||
		strings.EqualFold(
			obs.manifestImplementationTitle,
			paperManifestYouerToken,
		) ||
		strings.Contains(
			strings.ToLower(obs.manifestMainClass),
			paperMainClassYouerToken,
		) {
		add("youer")
	}

	slices.Sort(lineages)
	return lineages
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
	return strings.EqualFold(
		signals.specificationTitle,
		paperManifestYouerToken,
	) ||
		strings.EqualFold(signals.implementationTitle, paperManifestYouerToken) ||
		strings.Contains(
			strings.ToLower(signals.mainClass),
			paperMainClassYouerToken,
		)
}

func hasStrictReaperObservationLineage(obs paperObservations) bool {
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

	if judgment.lineageResult == lineageContradiction {
		judgment.familyResult = familyContradiction
		judgment.contradictionState = fmt.Sprintf(
			"lineage contradiction after bukkit confirmation: %s",
			nonEmptyPaperLineageName(judgment.lineageName, "unknown"),
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
	gameVersion types.Version,
	judgment paperJudgment,
) *RuntimeEvidence {
	if !judgment.bukkitConfirmed {
		return nil
	}

	lineage := "bukkit"

	if judgment.contradictionState != "" {
		judgment.addReason("runtime projection withheld paper promotion due to contradiction state")
	} else {
		switch judgment.lineageResult {
		case lineagePaper:
			lineage = "paper"
		case lineageFork:
			if detectedLineage := normalizePaperLineageName(judgment.lineageName); detectedLineage != "" {
				lineage = detectedLineage
			} else {
				judgment.addReason("paper-fork result without a concrete lineage retains bukkit baseline")
			}
		case lineageUnknown:
			switch judgment.familyResult {
			case familyStrong:
				judgment.addReason("strong paper-family evidence without a concrete lineage retains bukkit baseline")
			case familyWeak:
				judgment.addReason("weak paper-family evidence without a concrete lineage retains bukkit baseline")
			default:
				judgment.addReason("family miss remains non-terminal but projects to baseline bukkit runtime")
			}
		case lineageContradiction:
			// contradictionState is set by resolvePaperContradictions; projection
			// already withheld above via the contradictionState guard. This arm
			// makes the switch exhaustive over all paperLineageResult values.
			judgment.addReason("lineage contradiction: projection falls back to bukkit baseline")
		default:
		}
	}

	return &RuntimeEvidence{
		PrimaryPath: filePath, PrimaryRuntime: &types.VersionedPackageRef{
			Ecosystem: types.EcosystemUnspecified,
			Name:      input.ToProjectName(lineage),
			Version:   types.VersionUnknown,
		}, RuntimeComponents: []types.VersionedPackageRef{
			{
				Ecosystem: types.EcosystemMinecraft,
				Name:      input.ToProjectName("minecraft"),
				Version:   gameVersion,
			},
		}, DetectorName: (&craftBukkitFamilyDetector{}).Name(),
	}
}

func normalizePaperLineageName(name string) string {
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

func nonEmptyPaperLineageName(name string, fallback string) string {
	if normalized := normalizePaperLineageName(name); normalized != "" {
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

func parseBukkitGameVersion(implementationVersion string) types.Version {
	match := bukkitVersionPrefixPattern.FindStringSubmatch(strings.TrimSpace(implementationVersion))
	if len(match) < 2 || !isMinecraftReleaseVersion(match[1]) {
		return types.VersionUnknown
	}
	return types.Version(match[1])
}

func init() {
	registerExecutableDetector(&craftBukkitFamilyDetector{})
}
