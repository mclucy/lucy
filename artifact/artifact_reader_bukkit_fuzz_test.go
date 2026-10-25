package artifact_test

import (
	"strings"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/version"
)

// FuzzReadInfoBukkit exercises plugin.yml decoding through ReadInfo. The
// generated descriptor keeps one required and one optional dependency while
// varying the platform markers and descriptor validity.
func FuzzReadInfoBukkit(f *testing.F) {
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 0, 0, false)
	f.Add("demo", " 2.0.0 ", "com.example.Main", "dep", "opt", 1, 0, true)
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 2, 0, false)
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 3, 0, true)
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 4, 0, false)
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 5, 0, false)
	f.Add("demo", "   ", "com.example.Main", "dep", "opt", 0, 0, false)
	f.Add("demo", "1.0.0", "   ", "dep", "opt", 0, 0, false)
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 1, 1, false)
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 0, 2, false)
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 0, 3, false)
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 0, 4, false)
	f.Add("demo", "1.0.0", "com.example.Main", "dep", "opt", 0, 5, false)

	path := fuzzArchivePath(f, "artifact.jar")
	f.Fuzz(func(
		t *testing.T,
		rawName, rawVersion, rawMain, rawDependency, rawOptional string,
		rawSignal, rawFault int,
		folia bool,
	) {
		signal := modulo(rawSignal, 6)
		fault := modulo(rawFault, 6)
		name := "plugin-" + safeID(rawName, "demo")
		pluginVersion := printableText(rawVersion, 64)
		mainClass := printableText(rawMain, 64)
		requiredDependency := "required-" + safeID(rawDependency, "dep")
		optionalDependency := "optional-" + safeID(rawOptional, "opt")

		required := []string{requiredDependency}
		optional := []string{optionalDependency}
		descriptor := map[string]any{
			"name":            name,
			"version":         pluginVersion,
			"main":            mainClass,
			"description":     "Bukkit fixture",
			"folia-supported": folia,
		}
		switch signal {
		case 1:
			descriptor["paper-plugin-loader"] = "paper"
		case 2:
			descriptor["libraries"] = []string{"com.example:fixture:1.0"}
		case 3:
			required = []string{"paper", requiredDependency}
		case 4:
			optional = []string{"folia", optionalDependency}
		case 5:
			descriptor["api"] = []string{"leaves"}
		}
		descriptor["depend"] = required
		descriptor["softdepend"] = optional

		var descriptorBytes []byte
		switch fault {
		case 1:
			delete(descriptor, "name")
		case 2:
			delete(descriptor, "version")
		case 3:
			delete(descriptor, "main")
		case 4:
			descriptorBytes = []byte("name: [unterminated\n")
		case 5:
			descriptor["depend"] = requiredDependency
		}
		if descriptorBytes == nil {
			descriptorBytes = yamlFixture(t, descriptor)
		}
		writeZipFixture(t, path, zipFixtureEntry{
			name: "plugin.yml",
			data: descriptorBytes,
		})

		infos, readErr := artifact.ReadInfo(path)

		trimmedVersion := strings.TrimSpace(pluginVersion)
		trimmedMain := strings.TrimSpace(mainClass)
		if fault != 0 || trimmedVersion == "" || trimmedMain == "" {
			if len(infos) != 0 {
				t.Fatalf(
					"invalid plugin.yml (fault %d, name %q) yielded packages: %+v",
					fault, name, infos,
				)
			}
			return
		}
		if readErr != nil {
			t.Fatalf("ReadInfo() error = %v", readErr)
		}
		if len(infos) != 1 {
			t.Fatalf("ReadInfo() packages = %d, want 1: %+v", len(infos), infos)
		}
		info := infos[0]
		if got := string(info.Ref.Name); got != name {
			t.Fatalf("package identity = %q, want %q", got, name)
		}
		if info.Ref.Version != version.Version(trimmedVersion) {
			t.Fatalf("package version = %q, want %q", info.Ref.Version, trimmedVersion)
		}
		expectedPlatform := catalog.EcosystemBukkit
		if signal != 0 {
			expectedPlatform = catalog.EcosystemPaper
		}
		if info.Ref.Ecosystem != expectedPlatform {
			t.Fatalf("package ecosystem = %q, want %q", info.Ref.Ecosystem, expectedPlatform)
		}
		if info.Ref.Provider != catalog.ProviderUnknown {
			t.Fatalf("package provider = %q, want unknown", info.Ref.Provider)
		}
		if info.FilePath != path {
			t.Fatalf("package path = %q, want %q", info.FilePath, path)
		}
		if info.Metadata.Title != name {
			t.Fatalf("package title = %q, want %q", info.Metadata.Title, name)
		}
		if info.Compatibility.FoliaSupported != folia {
			t.Fatalf(
				"folia-supported = %v, want %v",
				info.Compatibility.FoliaSupported, folia,
			)
		}

		expectedDependencies := make(map[string]bool, len(required)+len(optional))
		for _, dependency := range required {
			expectedDependencies[dependency] = true
		}
		for _, dependency := range optional {
			expectedDependencies[dependency] = false
		}
		if len(info.Dependencies) != len(expectedDependencies) {
			t.Fatalf(
				"dependencies = %d, want %d: %+v",
				len(info.Dependencies), len(expectedDependencies), info.Dependencies,
			)
		}
		seen := make(map[string]struct{}, len(info.Dependencies))
		for _, dependency := range info.Dependencies {
			id := string(dependency.Ref.Name)
			mandatory, found := expectedDependencies[id]
			if !found {
				t.Fatalf("unexpected dependency %q", id)
			}
			if _, duplicate := seen[id]; duplicate {
				t.Fatalf("duplicate dependency %q", id)
			}
			seen[id] = struct{}{}
			if dependency.Mandatory != mandatory {
				t.Fatalf("dependency %q mandatory = %v, want %v", id, dependency.Mandatory, mandatory)
			}
			if dependency.Ref.Ecosystem != expectedPlatform {
				t.Fatalf(
					"dependency %q ecosystem = %q, want %q",
					id, dependency.Ref.Ecosystem, expectedPlatform,
				)
			}
			if len(dependency.Constraint) != 0 {
				t.Fatalf("dependency %q unexpectedly carries a constraint: %+v", id, dependency.Constraint)
			}
		}
	})
}
