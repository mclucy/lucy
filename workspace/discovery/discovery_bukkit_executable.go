package discovery

import (
	"bufio"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/version"
)

const (
	bukkitManifestPath              = "META-INF/MANIFEST.MF"
	bukkitManifestMainClass         = "org.bukkit.craftbukkit.Main"
	bukkitImplementationCraftBukkit = "CraftBukkit"
	bukkitPaperClassPrefix          = "io/papermc/paper/"
	bukkitLegacyPaperClassPrefix    = "com/destroystokyo/paper/"
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
// this package can emit. Youer is included even though it is a NeoForge
// hybrid; treat it separately before calling.
func IsPaperLineage(name string) bool {
	return paperLineages[name]
}

type bukkitManifestSignals struct {
	mainClass            string
	specificationTitle   string
	specificationVendor  string
	implementationTitle  string
	implementationVendor string
	implementationVer    string
}

// discoverBukkit recognizes CraftBukkit-derived server layouts. A single
// confirmed artifact is one installation; contradictory paper-lineage claims
// are returned as separate installations so the engine can report typed
// ambiguity instead of inventing a runtime.
func discoverBukkit(s *scan) ([]Installation, []Finding) {
	var installations []Installation
	var findings []Finding
	for _, primary := range s.RootFiles() {
		if !productCandidate(s, primary) {
			continue
		}
		detected, err := inspectBukkitInstallation(s, primary)
		if err != nil {
			findings = append(findings, Finding{
				Kind:  FindingUnreadableFile,
				Path:  primary.Path(),
				Cause: err,
			})
			continue
		}
		installations = append(installations, detected...)
	}
	return installations, findings
}

func inspectBukkitInstallation(
	s *scan,
	primary *artifact,
) ([]Installation, error) {
	var judgment paperJudgment

	manifest, ok, err := readBukkitManifest(s, primary)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	signals := parseBukkitManifest(manifest)
	metaMainClass, err := readBukkitSidecarString(
		s,
		primary,
		paperMetaMainClassPath,
	)
	if err != nil {
		return nil, err
	}
	reaperPatchProperties, err := readBukkitPatchProperties(s, primary)
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

	// Stage 2: Observation Extraction
	judgment.observations, err = extractPaperObservations(s, primary)
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
		gameVersion = version.Unknown
	}
	return projectPaperInstallations(primary.Path(), gameVersion, judgment), nil
}

func reasonPaperFamily(judgment *paperJudgment) {
	if judgment == nil {
		return
	}

	switch {
	case judgment.observations.hasPaperClasses:
		judgment.familyResult = familyStrong
	case hasModernPaperclipMetadataCluster(judgment.observations):
		judgment.familyResult = familyStrong
	case hasConcreteVersion(judgment.observations.versionJSONID):
		judgment.familyResult = familyWeak
	case len(judgment.observations.patchProperties) > 0 || judgment.observations.hasPaperMCPatch:
		judgment.familyResult = familyWeak
	case judgment.observations.hasPaperclipNamespace || judgment.observations.hasLegacyPaperclipNamespace:
		judgment.familyResult = familyWeak
	default:
		judgment.familyResult = familyMiss
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
	judgment.lineages = lineages

	switch {
	case len(lineages) > 1:
		judgment.lineageResult = lineageContradiction
		judgment.lineageName = strings.Join(lineages, ",")
	case len(lineages) == 1 && lineages[0] == "paper":
		judgment.lineageResult = lineagePaper
		judgment.lineageName = "paper"
	case len(lineages) == 1:
		judgment.lineageResult = lineageFork
		judgment.lineageName = lineages[0]
	default:
		judgment.lineageResult = lineageUnknown
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

	// Fixture citation: workspace/discovery/testdata/paper_family/test_paper/paper/META-INF/libraries.list
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryPaperToken,
	) {
		add("paper")
	}
	// Fixture citation: workspace/discovery/testdata/paper_family/test_folia/folia/META-INF/libraries.list
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryFoliaToken,
	) {
		add("folia")
	}
	// Fixture citation: workspace/discovery/testdata/paper_family/test_divine/divine/META-INF/libraries.list
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryDivineToken,
	) {
		add("divine")
	}
	// Fixture citation: workspace/discovery/testdata/paper_family/test_purpur/purpur/META-INF/libraries.list
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryPurpurToken,
	) {
		add("purpur")
	}
	// Fixture citation: workspace/discovery/testdata/paper_family/test_leaf/leaf/META-INF/libraries.list and cn/dreeam/leaper/*
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryLeafToken,
	) || obs.hasLeaperNamespace {
		add("leaf")
	}
	// Fixture citation: workspace/discovery/testdata/paper_family/test_leaves/leaves/META-INF/libraries.list, META-INF/build-info, META-INF/leavesclip-version
	if observationLinesContain(
		obs.librariesListEntries,
		paperLibraryLeavesToken,
	) || obs.hasLeavesclipNamespace || obs.leavesclipVersion != "" || strings.HasPrefix(
		obs.buildInfo,
		"Leaves\t",
	) {
		add("leaves")
	}
	// Fixture citation: workspace/discovery/testdata/paper_family/test_reaper/reaper/patch.properties
	if hasStrictReaperObservationLineage(obs) {
		add("reaper")
	}
	// Fixture citation: workspace/discovery/testdata/paper_family/test_youer/youer/META-INF/MANIFEST.MF
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

func readBukkitManifest(s *scan, primary *artifact) ([]byte, bool, error) {
	return readProductEntry(s, primary, bukkitManifestPath)
}

func readBukkitSidecarString(
	s *scan,
	primary *artifact,
	entryPath string,
) (string, error) {
	data, ok, err := readProductEntry(s, primary, entryPath)
	if err != nil || !ok {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func readBukkitPatchProperties(
	s *scan,
	primary *artifact,
) (map[string]string, error) {
	data, ok, err := readProductEntry(s, primary, paperPatchPropertiesPath)
	if err != nil || !ok {
		return nil, err
	}
	return parsePaperPatchProperties(data), nil
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
	}
	judgment.contradicted = judgment.familyResult == familyContradiction
}

// projectPaperInstallations emits one installation per attributable lineage.
// Contradictory claims are never merged into one fabricated identity; the
// engine reports the ambiguity and strict selection refuses it.
func projectPaperInstallations(
	filePath string,
	gameVersion version.Version,
	judgment paperJudgment,
) []Installation {
	if !judgment.bukkitConfirmed {
		return nil
	}

	components := []catalog.VersionedRef{
		{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      input.ToProjectName("minecraft"),
			Version:   gameVersion,
		},
	}

	if judgment.contradicted {
		installations := make([]Installation, 0, len(judgment.lineages))
		for _, lineage := range judgment.lineages {
			name := normalizePaperLineageName(lineage)
			if name == "" {
				continue
			}
			installations = append(
				installations,
				newPaperInstallation(name, components, filePath),
			)
		}
		if len(installations) > 0 {
			return installations
		}
	}

	lineage := "bukkit"
	switch judgment.lineageResult {
	case lineagePaper:
		lineage = "paper"
	case lineageFork:
		if detected := normalizePaperLineageName(judgment.lineageName); detected != "" {
			lineage = detected
		}
	case lineageUnknown, lineageContradiction:
		// No attributable lineage retains the confirmed bukkit baseline.
	}

	return []Installation{newPaperInstallation(lineage, components, filePath)}
}

func newPaperInstallation(
	lineage string,
	components []catalog.VersionedRef,
	filePath string,
) Installation {
	return NewInstallation(
		Identity{
			Primary: catalog.VersionedRef{
				Ecosystem: paperLineageEcosystem(lineage),
				Name:      input.ToProjectName(lineage),
				Version:   version.Unknown,
			},
			Components: cloneComponents(components),
		},
		filePath,
	)
}

func paperLineageEcosystem(lineage string) catalog.Ecosystem {
	switch lineage {
	case "bukkit":
		return catalog.EcosystemBukkit
	case "youer":
		// Youer is a NeoForge hybrid; its primary product is not a Paper
		// ecosystem, and served-package policy handles it by name.
		return catalog.EcosystemUnspecified
	default:
		return catalog.EcosystemPaper
	}
}

func cloneComponents(
	components []catalog.VersionedRef,
) []catalog.VersionedRef {
	if len(components) == 0 {
		return nil
	}
	return append([]catalog.VersionedRef(nil), components...)
}

// observedVersion keeps an unparseable or absent artifact version observable
// as version.Unknown instead of producing an invalid identity.
func observedVersion(value version.Version) version.Version {
	if !hasConcreteVersion(value) {
		return version.Unknown
	}
	return value
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

func parseBukkitGameVersion(implementationVersion string) version.Version {
	match := bukkitVersionPrefixPattern.FindStringSubmatch(strings.TrimSpace(implementationVersion))
	if len(match) < 2 || !isMinecraftReleaseVersion(match[1]) {
		return version.Unknown
	}
	return version.Version(match[1])
}

// productCandidate reports whether a root artifact can carry an executable
// server layout. Directory layouts are only considered when they expose the
// manifest every recognized server product ships.
func productCandidate(s *scan, primary *artifact) bool {
	if primary == nil {
		return false
	}
	if !primary.IsDirectory() {
		return strings.HasSuffix(
			strings.ToLower(primary.Path()),
			".jar",
		)
	}
	manifest, ok := s.File(
		filepath.Join(
			primary.Path(),
			filepath.FromSlash(bukkitManifestPath),
		),
	)
	return ok && !manifest.IsDirectory()
}

// readProductEntry reads one entry from a jar artifact or from an unpacked
// directory layout artifact. Directory reads stay root-confined through the
// scan; archives use the scan-owned ReaderAt handle.
func readProductEntry(
	s *scan,
	primary *artifact,
	entryPath string,
) ([]byte, bool, error) {
	if primary == nil {
		return nil, false, nil
	}
	if primary.IsDirectory() {
		nested, ok := s.File(
			filepath.Join(
				primary.Path(),
				filepath.FromSlash(entryPath),
			),
		)
		if !ok || nested.IsDirectory() {
			return nil, false, nil
		}
		data, err := nested.Read()
		if err != nil {
			return nil, false, err
		}
		return data, true, nil
	}

	archive, err := primary.Archive()
	if err != nil {
		return nil, false, err
	}
	if archive == nil {
		return nil, false, nil
	}
	return readArchiveEntry(archive, entryPath)
}

// productEntryExists reports whether one entry is present without loading its
// bytes. Large payload jars are never read just to test for a marker.
func productEntryExists(
	s *scan,
	primary *artifact,
	entryPath string,
) (bool, error) {
	if primary == nil {
		return false, nil
	}
	if primary.IsDirectory() {
		nested, ok := s.File(
			filepath.Join(
				primary.Path(),
				filepath.FromSlash(entryPath),
			),
		)
		return ok && !nested.IsDirectory(), nil
	}

	archive, err := primary.Archive()
	if err != nil {
		return false, err
	}
	if archive == nil {
		return false, nil
	}
	for _, file := range archive.File {
		if file.Name == entryPath {
			return true, nil
		}
	}
	return false, nil
}
