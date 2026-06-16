package init

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
	"github.com/pelletier/go-toml"
	"gopkg.in/yaml.v3"
)

type DiscoveryConfidence string

const (
	ConfidenceHigh   DiscoveryConfidence = "high"
	ConfidenceMedium DiscoveryConfidence = "medium"
	ConfidenceLow    DiscoveryConfidence = "low"
	ConfidenceNone   DiscoveryConfidence = "none"
)

type DiscoveredDefaults struct {
	GameVersion            string
	Platform               string
	PlatformVersion        string
	DetectedPackages       []string
	PackageClassifications []TakeoverPackageClassification
	Confidence             DiscoveryConfidence
	ExistingLucy           ExistingLucyHints
}

// ExistingLucyHints captures pre-existing Lucy state as advisory context.
// Under takeover-first init, these hints may fill observation gaps or explain
// drift, but they must not silently outrank live observed state.
type ExistingLucyHints struct {
	GameVersion     string
	Platform        string
	PlatformVersion string
	ConfigPresent   bool
	ManifestPresent bool
	LockPresent     bool
}

func (h ExistingLucyHints) HasAny() bool {
	return h.ConfigPresent || h.ManifestPresent || h.LockPresent || h.GameVersion != "" || h.Platform != "" || h.PlatformVersion != ""
}

// DiscoverServerDefaults is the takeover aggregator used by the current init
// flow. Contractually, takeover-class init must aggregate current server
// information before it proposes desired intent:
//   - discovery-first is only about sequence: discover before asking
//   - discovery-led is about behavior: observed facts become the primary input
//     to the proposal, and stale state files are demoted to advisory hints
//
// probe.ServerInfoAt(workDir) now provides the primary observed-state layer so
// takeover candidates come from the richer probe/runtime model first. Local
// file/archive heuristics remain fallback-only for gaps the probe could not
// explain. Existing state is recorded separately and only fills gaps when
// no live observation is available.
func DiscoverServerDefaults(workDir string) DiscoveredDefaults {
	defaults := DiscoveredDefaults{Confidence: ConfidenceNone}
	applyObservedDefaults(&defaults, workDir, probe.ServerInfoAt(workDir))

	if version := discoverGameVersion(workDir); version != "" {
		if defaults.GameVersion == "" {
			defaults.GameVersion = version
		}
		defaults.Confidence = maxConfidence(
			defaults.Confidence,
			ConfidenceMedium,
		)
	}

	platform, platformVersion, platformConfidence := discoverPlatform(workDir)
	if platform != "" && defaults.Platform == "" {
		defaults.Platform = platform
		defaults.Confidence = maxConfidence(
			defaults.Confidence,
			platformConfidence,
		)
	}
	if platformVersion != "" && defaults.PlatformVersion == "" {
		defaults.PlatformVersion = platformVersion
		defaults.Confidence = maxConfidence(
			defaults.Confidence,
			platformConfidence,
		)
	}

	defaults.DetectedPackages = appendUnique(
		defaults.DetectedPackages,
		discoverPackages(workDir)...,
	)
	if len(defaults.DetectedPackages) > 0 && defaults.Confidence == ConfidenceNone {
		defaults.Confidence = ConfidenceLow
	}

	manifest, manifestExists, manifestErr := state.ReadManifest(workDir)
	if manifestErr == nil && manifestExists && manifest != nil {
		defaults.ExistingLucy.ManifestPresent = true
		defaults.ExistingLucy.GameVersion = strings.TrimSpace(manifest.Environment.GameVersion)
		defaults.ExistingLucy.Platform = strings.TrimSpace(manifest.Environment.ModdingPlatform)
		defaults.ExistingLucy.PlatformVersion = strings.TrimSpace(manifest.Environment.ModdingPlatformVersion)
	}

	config, configExists, configErr := state.ReadConfig(workDir)
	if configErr == nil && configExists && config != nil {
		defaults.ExistingLucy.ConfigPresent = true
	}

	if _, lockExists, lockErr := state.ReadLock(workDir); lockErr == nil && lockExists {
		defaults.ExistingLucy.LockPresent = true
	}

	if defaults.GameVersion == "" {
		defaults.GameVersion = defaults.ExistingLucy.GameVersion
	}
	if defaults.Platform == "" {
		defaults.Platform = defaults.ExistingLucy.Platform
	}
	if defaults.PlatformVersion == "" {
		defaults.PlatformVersion = defaults.ExistingLucy.PlatformVersion
	}
	if defaults.Confidence == ConfidenceNone && defaults.ExistingLucy.HasAny() {
		defaults.Confidence = ConfidenceLow
	}

	return defaults
}

func applyObservedDefaults(
	defaults *DiscoveredDefaults,
	workDir string,
	observed probe.Workspace,
) {
	if defaults == nil {
		return
	}

	if runtime := observed.Runtime; runtime != nil {
		if gameVersion := sanitizeObservedVersion(runtime.GameVersion.String()); gameVersion != "" {
			defaults.GameVersion = gameVersion
			defaults.Confidence = maxConfidence(
				defaults.Confidence,
				ConfidenceHigh,
			)
		}
		if platform := runtime.DerivedModLoader(); platform.Valid() && platform != types.PlatformMinecraft {
			defaults.Platform = string(platform)
			defaults.Confidence = maxConfidence(
				defaults.Confidence,
				ConfidenceHigh,
			)
			if identity := runtimeIdentityPackage(string(platform)); identity != "" {
				defaults.DetectedPackages = appendUnique(
					defaults.DetectedPackages,
					identity,
				)
			}
		}
		if version := sanitizeObservedVersion(runtime.DerivedLoaderVersion()); version != "" {
			defaults.PlatformVersion = version
			defaults.Confidence = maxConfidence(
				defaults.Confidence,
				ConfidenceHigh,
			)
		} else if defaults.Platform != "" {
			if version := discoverObservedLoaderVersion(
				workDir,
				types.PlatformId(defaults.Platform),
			); version != "" {
				defaults.PlatformVersion = version
				defaults.Confidence = maxConfidence(
					defaults.Confidence,
					ConfidenceHigh,
				)
			}
		}
	}

	defaults.PackageClassifications = BuildTakeoverPackageClassifications(observed.Packages)
	defaults.DetectedPackages = appendUnique(
		defaults.DetectedPackages,
		packageCandidatesFromObserved(observed.Packages)...,
	)
	for _, classification := range defaults.PackageClassifications {
		defaults.DetectedPackages = appendUnique(
			defaults.DetectedPackages,
			classification.ID,
		)
	}
	if len(defaults.DetectedPackages) > 0 || len(defaults.PackageClassifications) > 0 {
		defaults.Confidence = maxConfidence(defaults.Confidence, ConfidenceHigh)
	}
}

func packageCandidatesFromObserved(packages []types.Package) []string {
	candidates := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if pkg.Id.Platform == types.PlatformAny || pkg.Id.Platform == types.PlatformUnknown || strings.TrimSpace(pkg.Id.Name.String()) == "" {
			continue
		}
		candidates = append(candidates, pkg.Id.StringBase())
	}
	sort.Strings(candidates)
	return uniqueStrings(candidates)
}

func runtimeIdentityPackage(platform string) string {
	p := types.PlatformId(strings.TrimSpace(platform))
	if p == types.PlatformNone || !p.Valid() {
		return ""
	}
	return types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: p,
			Name:     types.BarePackageName(p.String()),
		},
	}.StringBase()
}

func discoverObservedLoaderVersion(
	workDir string,
	platform types.PlatformId,
) string {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(
			strings.ToLower(entry.Name()),
			".jar",
		) {
			continue
		}
		version := discoverLoaderVersion(
			filepath.Join(workDir, entry.Name()),
			platform,
		)
		if version != "" {
			return version
		}
	}
	return ""
}

func sanitizeObservedVersion(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "none", "unknown":
		return ""
	default:
		return value
	}
}

func ApplyDiscoveredDefaults(s *InitFlowState, defaults DiscoveredDefaults) {
	if strings.TrimSpace(s.GameVersion) == "" {
		s.GameVersion = strings.TrimSpace(defaults.GameVersion)
	}
	if strings.TrimSpace(s.Platform) == "" {
		s.Platform = strings.TrimSpace(defaults.Platform)
	}
	if strings.TrimSpace(s.PlatformVersion) == "" {
		s.PlatformVersion = strings.TrimSpace(defaults.PlatformVersion)
	}
	if len(s.PackageClassifications) == 0 {
		s.PackageClassifications = append(
			[]TakeoverPackageClassification(nil),
			defaults.PackageClassifications...,
		)
	}
}

func discoverGameVersion(workDir string) string {
	for _, candidate := range []string{"server.properties", "eula.txt"} {
		path := filepath.Join(workDir, candidate)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(key), "version") {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func discoverPlatform(workDir string) (string, string, DiscoveryConfidence) {
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return "", "", ConfidenceNone
	}

	hasLibraries := dirExists(filepath.Join(workDir, "libraries"))
	hasRunScript := fileExists(
		filepath.Join(
			workDir,
			"run.sh",
		),
	) || fileExists(filepath.Join(workDir, "run.bat"))
	hasMcdrConfig := fileExists(
		filepath.Join(
			workDir,
			"pyproject.toml",
		),
	) || dirExists(filepath.Join(workDir, "mcs_config"))

	if hasMcdrConfig {
		return string(types.PlatformMCDR), "", ConfidenceMedium
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		switch {
		case strings.HasPrefix(name, "fabric-server") && strings.HasSuffix(
			name,
			".jar",
		):
			return string(types.PlatformFabric), "", ConfidenceHigh
		case strings.Contains(name, "fabric") && strings.HasSuffix(
			name,
			".jar",
		):
			return string(types.PlatformFabric), "", ConfidenceMedium
		case strings.Contains(name, "neoforge") && strings.HasSuffix(
			name,
			".jar",
		):
			return string(types.PlatformNeoforge), "", ConfidenceHigh
		case strings.Contains(name, "forge") && strings.HasSuffix(name, ".jar"):
			return string(types.PlatformForge), "", ConfidenceHigh
		case name == "server.jar":
			return string(types.PlatformNone), "", ConfidenceLow
		}
	}

	if hasLibraries && hasRunScript {
		return string(types.PlatformForge), "", ConfidenceMedium
	}

	if dirExists(filepath.Join(workDir, "mods")) {
		return string(types.PlatformFabric), "", ConfidenceLow
	}
	if dirExists(filepath.Join(workDir, "plugins")) {
		return string(types.PlatformNone), "", ConfidenceLow
	}

	return "", "", ConfidenceNone
}

func discoverLoaderVersion(path string, platform types.PlatformId) string {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer reader.Close()

	manifest, ok := readArchiveFile(&reader.Reader, "META-INF/MANIFEST.MF")
	if !ok {
		return ""
	}

	for line := range strings.SplitSeq(string(manifest), "\n") {
		line = strings.TrimSpace(line)
		switch platform {
		case types.PlatformFabric:
			if version := trimVersionFromManifestClasspath(
				line,
				"libraries/net/fabricmc/fabric-loader/",
			); version != "" {
				return version
			}
		case types.PlatformNeoforge:
			if version := trimVersionFromManifestClasspath(
				line,
				"libraries/net/neoforged/neoforge/",
			); version != "" {
				return version
			}
		case types.PlatformForge:
			if version := trimVersionFromManifestClasspath(
				line,
				"libraries/net/minecraftforge/forge/",
			); version != "" {
				return version
			}
		}
	}

	return ""
}

func trimVersionFromManifestClasspath(line, marker string) string {
	_, rest, ok := strings.Cut(line, marker)
	if !ok {
		return ""
	}
	version, _, _ := strings.Cut(rest, "/")
	return sanitizeObservedVersion(version)
}

func discoverPackages(workDir string) []string {
	packages := make([]string, 0)
	for _, root := range []string{"mods", "plugins"} {
		dir := filepath.Join(workDir, root)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			packages = appendUnique(packages, detectPackageIDs(path)...)
		}
	}
	sort.Strings(packages)
	return packages
}

func detectPackageIDs(path string) []string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".jar"), strings.HasSuffix(
		lower,
		".pyz",
	), strings.HasSuffix(lower, ".mcdr"):
		return detectArchivePackages(path)
	case strings.HasSuffix(lower, ".jar.disabled"):
		return detectArchivePackages(path)
	default:
		return nil
	}
}

func detectArchivePackages(path string) []string {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	defer reader.Close()

	packages := make([]string, 0)
	if fabricMeta, ok := readArchiveFile(
		&reader.Reader,
		"fabric.mod.json",
	); ok {
		var mod exttype.FileFabricModIdentifier
		if json.Unmarshal(
			fabricMeta,
			&mod,
		) == nil && strings.TrimSpace(mod.Id) != "" {
			packages = append(
				packages,
				types.VersionedPackageRef{
					PackageRef: types.PackageRef{
						Platform: types.PlatformFabric,
						Name:     types.BarePackageName(strings.TrimSpace(mod.Id)),
					},
				}.StringBase(),
			)
		}
	}

	if neoMeta, ok := readArchiveFile(
		&reader.Reader,
		"META-INF/neoforge.mods.toml",
	); ok {
		var mod exttype.FileModLoaderIdentifier
		if toml.Unmarshal(neoMeta, &mod) == nil {
			for _, item := range mod.Mods {
				if strings.TrimSpace(item.ModID) == "" {
					continue
				}
				packages = append(
					packages,
					types.VersionedPackageRef{
						PackageRef: types.PackageRef{
							Platform: types.PlatformNeoforge,
							Name:     types.BarePackageName(strings.TrimSpace(item.ModID)),
						},
					}.StringBase(),
				)
			}
		}
	}

	if forgeMeta, ok := readArchiveFile(
		&reader.Reader,
		"META-INF/mods.toml",
	); ok {
		var mod exttype.FileModLoaderIdentifier
		if toml.Unmarshal(forgeMeta, &mod) == nil {
			for _, item := range mod.Mods {
				if strings.TrimSpace(item.ModID) == "" {
					continue
				}
				packages = append(
					packages,
					types.VersionedPackageRef{
						PackageRef: types.PackageRef{
							Platform: types.PlatformForge,
							Name:     types.BarePackageName(strings.TrimSpace(item.ModID)),
						},
					}.StringBase(),
				)
			}
		}
	}

	if oldForgeMeta, ok := readArchiveFile(&reader.Reader, "mcmod.info"); ok {
		var mods exttype.FileForgeModIdentifierOld
		if json.Unmarshal(oldForgeMeta, &mods) == nil {
			for _, item := range mods {
				if strings.TrimSpace(item.ModId) == "" {
					continue
				}
				packages = append(
					packages,
					types.VersionedPackageRef{
						PackageRef: types.PackageRef{
							Platform: types.PlatformForge,
							Name:     types.BarePackageName(strings.TrimSpace(item.ModId)),
						},
					}.StringBase(),
				)
			}
		}
	}

	if mcdrMeta, ok := readArchiveFile(
		&reader.Reader,
		"mcdreforged.plugin.json",
	); ok {
		var plugin exttype.FileMcdrPluginIdentifier
		if json.Unmarshal(
			mcdrMeta,
			&plugin,
		) == nil && strings.TrimSpace(plugin.Id) != "" {
			packages = append(
				packages,
				types.VersionedPackageRef{
					PackageRef: types.PackageRef{
						Platform: types.PlatformMCDR,
						Name:     types.BarePackageName(strings.TrimSpace(plugin.Id)),
					},
				}.StringBase(),
			)
		}
	}

	if pluginMeta, ok := readArchiveFile(&reader.Reader, "plugin.yml"); ok {
		if id := parsePluginYAMLName(pluginMeta); id != "" {
			packages = append(
				packages,
				types.VersionedPackageRef{
					PackageRef: types.PackageRef{
						Platform: types.PlatformNone,
						Name:     types.BarePackageName(id),
					},
				}.StringBase(),
			)
		}
	}
	if paperMeta, ok := readArchiveFile(
		&reader.Reader,
		"paper-plugin.yml",
	); ok {
		if id := parsePluginYAMLName(paperMeta); id != "" {
			packages = append(
				packages,
				types.VersionedPackageRef{
					PackageRef: types.PackageRef{
						Platform: types.PlatformNone,
						Name:     types.BarePackageName(id),
					},
				}.StringBase(),
			)
		}
	}

	return uniqueStrings(packages)
}

func readArchiveFile(reader *zip.Reader, name string) ([]byte, bool) {
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, false
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, false
		}
		return data, true
	}
	return nil, false
}

func parsePluginYAMLName(data []byte) string {
	var parsed struct {
		Name string `yaml:"name"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&parsed); err != nil {
		return ""
	}
	return sanitizeProjectName(parsed.Name)
}

func sanitizeProjectName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-', r == '_', r == '.', r == ' ':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func appendUnique(values []string, extras ...string) []string {
	return uniqueStrings(append(values, extras...))
}

func maxConfidence(left, right DiscoveryConfidence) DiscoveryConfidence {
	if confidenceRank(right) > confidenceRank(left) {
		return right
	}
	return left
}

func confidenceRank(value DiscoveryConfidence) int {
	switch value {
	case ConfidenceHigh:
		return 4
	case ConfidenceMedium:
		return 3
	case ConfidenceLow:
		return 2
	case ConfidenceNone:
		return 1
	default:
		return 0
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func describeDiscovery(defaults DiscoveredDefaults) string {
	parts := make([]string, 0, 5)
	if defaults.GameVersion != "" {
		parts = append(parts, fmt.Sprintf("game=%s", defaults.GameVersion))
	}
	if defaults.Platform != "" {
		parts = append(parts, fmt.Sprintf("platform=%s", defaults.Platform))
	}
	if len(defaults.DetectedPackages) > 0 {
		parts = append(
			parts,
			fmt.Sprintf("packages=%d", len(defaults.DetectedPackages)),
		)
	}
	if defaults.ExistingLucy.HasAny() {
		parts = append(parts, "existing-state=advisory")
	}
	if len(parts) == 0 {
		return "No server defaults detected"
	}
	return fmt.Sprintf(
		"Detected defaults (%s confidence): %s",
		defaults.Confidence,
		strings.Join(parts, "; "),
	)
}
