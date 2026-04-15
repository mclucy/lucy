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
	GameVersion      string
	Platform         string
	PlatformVersion  string
	ManagedRoots     []string
	DetectedPackages []string
	Confidence       DiscoveryConfidence
}

func DiscoverServerDefaults(workDir string) DiscoveredDefaults {
	defaults := DiscoveredDefaults{Confidence: ConfidenceNone}

	manifest, manifestExists, manifestErr := state.ReadManifest(workDir)
	if manifestErr == nil && manifestExists && manifest != nil {
		defaults.GameVersion = strings.TrimSpace(manifest.Environment.GameVersion)
		defaults.Platform = strings.TrimSpace(manifest.Environment.Platform)
		defaults.PlatformVersion = strings.TrimSpace(manifest.Environment.PlatformVersion)
		defaults.ManagedRoots = appendUnique(defaults.ManagedRoots, manifest.Policy.ManagedRoots...)
		defaults.Confidence = maxConfidence(defaults.Confidence, ConfidenceHigh)
	}

	config, configExists, configErr := state.ReadConfig(workDir)
	if configErr == nil && configExists && config != nil {
		defaults.ManagedRoots = appendUnique(defaults.ManagedRoots, config.Scope.ManagedRoots...)
		defaults.Confidence = maxConfidence(defaults.Confidence, ConfidenceHigh)
	}

	if defaults.GameVersion == "" {
		if version := discoverGameVersion(workDir); version != "" {
			defaults.GameVersion = version
			defaults.Confidence = maxConfidence(defaults.Confidence, ConfidenceMedium)
		}
	}

	platform, platformVersion, platformConfidence := discoverPlatform(workDir)
	if defaults.Platform == "" && platform != "" {
		defaults.Platform = platform
		defaults.Confidence = maxConfidence(defaults.Confidence, platformConfidence)
	}
	if defaults.PlatformVersion == "" && platformVersion != "" {
		defaults.PlatformVersion = platformVersion
		defaults.Confidence = maxConfidence(defaults.Confidence, platformConfidence)
	}

	defaults.ManagedRoots = appendUnique(defaults.ManagedRoots, detectManagedRoots(workDir)...)
	if len(defaults.ManagedRoots) > 0 && defaults.Confidence == ConfidenceNone {
		defaults.Confidence = ConfidenceLow
	}

	defaults.DetectedPackages = discoverPackages(workDir)
	if len(defaults.DetectedPackages) > 0 && defaults.Confidence == ConfidenceNone {
		defaults.Confidence = ConfidenceLow
	}

	return defaults
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
	if len(s.ManagedRoots) == 0 {
		s.ManagedRoots = append([]string(nil), defaults.ManagedRoots...)
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
	hasRunScript := fileExists(filepath.Join(workDir, "run.sh")) || fileExists(filepath.Join(workDir, "run.bat"))
	hasMcdrConfig := fileExists(filepath.Join(workDir, "pyproject.toml")) || dirExists(filepath.Join(workDir, "mcs_config"))

	if hasMcdrConfig {
		return string(types.PlatformMCDR), "", ConfidenceMedium
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		switch {
		case strings.HasPrefix(name, "fabric-server") && strings.HasSuffix(name, ".jar"):
			return string(types.PlatformFabric), "", ConfidenceHigh
		case strings.Contains(name, "fabric") && strings.HasSuffix(name, ".jar"):
			return string(types.PlatformFabric), "", ConfidenceMedium
		case strings.Contains(name, "neoforge") && strings.HasSuffix(name, ".jar"):
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

func detectManagedRoots(workDir string) []string {
	roots := make([]string, 0, 5)
	for _, root := range []string{"mods", "plugins", "config", "datapacks", "resourcepacks", "kubejs"} {
		if dirExists(filepath.Join(workDir, root)) {
			roots = append(roots, root)
		}
	}
	return roots
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
	case strings.HasSuffix(lower, ".jar"), strings.HasSuffix(lower, ".pyz"), strings.HasSuffix(lower, ".mcdr"):
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
	if fabricMeta, ok := readArchiveFile(&reader.Reader, "fabric.mod.json"); ok {
		var mod exttype.FileFabricModIdentifier
		if json.Unmarshal(fabricMeta, &mod) == nil && strings.TrimSpace(mod.Id) != "" {
			packages = append(packages, types.PackageId{Platform: types.PlatformFabric, Name: types.ProjectName(strings.TrimSpace(mod.Id))}.StringPlatformName())
		}
	}

	if neoMeta, ok := readArchiveFile(&reader.Reader, "META-INF/neoforge.mods.toml"); ok {
		var mod exttype.FileModLoaderIdentifier
		if toml.Unmarshal(neoMeta, &mod) == nil {
			for _, item := range mod.Mods {
				if strings.TrimSpace(item.ModID) == "" {
					continue
				}
				packages = append(packages, types.PackageId{Platform: types.PlatformNeoforge, Name: types.ProjectName(strings.TrimSpace(item.ModID))}.StringPlatformName())
			}
		}
	}

	if forgeMeta, ok := readArchiveFile(&reader.Reader, "META-INF/mods.toml"); ok {
		var mod exttype.FileModLoaderIdentifier
		if toml.Unmarshal(forgeMeta, &mod) == nil {
			for _, item := range mod.Mods {
				if strings.TrimSpace(item.ModID) == "" {
					continue
				}
				packages = append(packages, types.PackageId{Platform: types.PlatformForge, Name: types.ProjectName(strings.TrimSpace(item.ModID))}.StringPlatformName())
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
				packages = append(packages, types.PackageId{Platform: types.PlatformForge, Name: types.ProjectName(strings.TrimSpace(item.ModId))}.StringPlatformName())
			}
		}
	}

	if mcdrMeta, ok := readArchiveFile(&reader.Reader, "mcdreforged.plugin.json"); ok {
		var plugin exttype.FileMcdrPluginIdentifier
		if json.Unmarshal(mcdrMeta, &plugin) == nil && strings.TrimSpace(plugin.Id) != "" {
			packages = append(packages, types.PackageId{Platform: types.PlatformMCDR, Name: types.ProjectName(strings.TrimSpace(plugin.Id))}.StringPlatformName())
		}
	}

	if pluginMeta, ok := readArchiveFile(&reader.Reader, "plugin.yml"); ok {
		if id := parsePluginYAMLName(pluginMeta); id != "" {
			packages = append(packages, types.PackageId{Platform: types.PlatformNone, Name: types.ProjectName(id)}.StringPlatformName())
		}
	}
	if paperMeta, ok := readArchiveFile(&reader.Reader, "paper-plugin.yml"); ok {
		if id := parsePluginYAMLName(paperMeta); id != "" {
			packages = append(packages, types.PackageId{Platform: types.PlatformNone, Name: types.ProjectName(id)}.StringPlatformName())
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
	if len(defaults.ManagedRoots) > 0 {
		parts = append(parts, fmt.Sprintf("roots=%s", strings.Join(defaults.ManagedRoots, ",")))
	}
	if len(defaults.DetectedPackages) > 0 {
		parts = append(parts, fmt.Sprintf("packages=%d", len(defaults.DetectedPackages)))
	}
	if len(parts) == 0 {
		return "No server defaults detected"
	}
	return fmt.Sprintf("Detected defaults (%s confidence): %s", defaults.Confidence, strings.Join(parts, "; "))
}
