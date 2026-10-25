package artifact_test

import (
	"strings"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/version"
)

type spongeFixtureMetadata struct {
	Loader  spongeFixtureLoader   `json:"loader"`
	License string                `json:"license,omitempty"`
	Global  spongeFixturePlugin   `json:"global"`
	Plugins []spongeFixturePlugin `json:"plugins"`
}

type spongeFixtureLoader struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type spongeFixturePlugin struct {
	ID           string                    `json:"id,omitempty"`
	Name         string                    `json:"name,omitempty"`
	Entrypoint   string                    `json:"entrypoint,omitempty"`
	Description  string                    `json:"description,omitempty"`
	Version      string                    `json:"version,omitempty"`
	Dependencies []spongeFixtureDependency `json:"dependencies,omitempty"`
}

type spongeFixtureDependency struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	LoadOrder string `json:"load-order,omitempty"`
	Optional  bool   `json:"optional,omitempty"`
}

// FuzzReadInfoSponge checks global metadata inheritance, per-plugin overrides,
// plugin skipping, and dependency translation from a real Sponge metadata JAR.
func FuzzReadInfoSponge(f *testing.F) {
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "[1.0,2.0)", true, 0)
	f.Add("demo", "", "9.9.9", "gamma", "[1.0,2.0)", false, 0)
	f.Add("demo", "1.0.0", "", "gamma", "*", false, 0)
	f.Add("demo", "   ", "9.9.9", "gamma", "[1.0,2.0)", true, 0)
	f.Add("demo", "", "", "gamma", "[1.0,2.0)", false, 0)
	f.Add("demo", " 1.0 ", " 9.9 ", "gamma", "(,1.0]", true, 0)
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "not a range", false, 0)
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "[1.0", false, 0)
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "[2.0,1.0]", false, 0)
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "", false, 0)
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "[1.0,2.0)", false, 1)
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "[1.0,2.0)", false, 2)
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "[1.0,2.0)", false, 3)
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "[1.0,2.0)", false, 4)
	f.Add("demo", "1.0.0", "9.9.9", "gamma", "[1.0,2.0)", false, 5)

	path := fuzzArchivePath(f, "artifact.jar")
	f.Fuzz(func(
		t *testing.T,
		rawID, rawPluginVersion, rawGlobalVersion, rawDependencyID, rawConstraint string,
		optional bool,
		rawFault int,
	) {
		fault := modulo(rawFault, 6)
		suffix := safeID(rawID, "demo")
		alphaID := "alpha-" + suffix
		betaID := "beta-" + suffix
		gammaID := "gamma-" + suffix
		pluginVersion := clippedText(rawPluginVersion, 64)
		globalVersion := clippedText(rawGlobalVersion, 64)
		globalDependencyID := "global-" + safeID(rawDependencyID, "dep")
		pluginDependencyID := "plugin-" + safeID(rawDependencyID, "dep")
		constraint := clippedText(rawConstraint, 128)

		globalDependency := spongeFixtureDependency{
			ID: globalDependencyID, Version: constraint, Optional: optional,
		}
		pluginDependency := spongeFixtureDependency{
			ID: pluginDependencyID, Version: constraint, Optional: optional,
		}
		spongeAPI := spongeFixtureDependency{ID: "spongeapi", Version: constraint}
		alpha := spongeFixturePlugin{
			ID: alphaID, Name: "Alpha Display", Entrypoint: "example.Alpha",
			Version: pluginVersion,
		}
		beta := spongeFixturePlugin{
			ID: betaID, Name: "Beta Display", Entrypoint: "example.Beta",
			Dependencies: []spongeFixtureDependency{pluginDependency, spongeAPI},
		}
		gamma := spongeFixturePlugin{ID: gammaID}
		metadata := spongeFixtureMetadata{
			Loader:  spongeFixtureLoader{Name: "java_plain", Version: "1.0"},
			License: "MIT",
			Global: spongeFixturePlugin{
				Version:      globalVersion,
				Dependencies: []spongeFixtureDependency{globalDependency, spongeAPI},
			},
			Plugins: []spongeFixturePlugin{alpha, beta, gamma},
		}

		var metadataBytes []byte
		switch fault {
		case 1:
			metadata.Loader.Name = ""
		case 2:
			metadataBytes = []byte(`{"loader":`)
		case 3:
			metadata.Plugins = nil
		case 4:
			metadata.Plugins[0].Entrypoint = ""
			metadata.Plugins[1].Entrypoint = ""
		case 5:
			metadata.Loader.Version = ""
		}
		if metadataBytes == nil {
			metadataBytes = jsonFixture(t, metadata)
		}
		writeZipFixture(t, path, zipFixtureEntry{
			name: "META-INF/sponge_plugins.json",
			data: metadataBytes,
		})

		infos, readErr := artifact.ReadInfo(path)

		if fault != 0 {
			if len(infos) != 0 {
				t.Fatalf("invalid Sponge metadata (fault %d) yielded packages: %+v", fault, infos)
			}
			return
		}

		engine, err := version.NewEngine(version.DialectMaven)
		if err != nil {
			t.Fatalf("NewEngine(maven) error = %v", err)
		}
		var baseConstraint version.Expr
		trimmedConstraint := strings.TrimSpace(constraint)
		if trimmedConstraint == "" {
			// Sponge requires dependency versions. Handling of a missing range
			// is not asserted because both rejection and omission are plausible
			// tolerant behaviors.
			return
		}
		baseConstraint, err = engine.ParseConstraint(constraint)
		if err != nil {
			// ReadInfo may reject or tolerate a malformed dependency range;
			// package identity tolerance is not part of this contract.
			return
		}

		alphaVersion := strings.TrimSpace(pluginVersion)
		if alphaVersion == "" {
			alphaVersion = strings.TrimSpace(globalVersion)
		}
		betaVersion := strings.TrimSpace(globalVersion)
		expectedVersions := make(map[string]string, 2)
		if alphaVersion != "" {
			expectedVersions[alphaID] = alphaVersion
		}
		if betaVersion != "" {
			expectedVersions[betaID] = betaVersion
		}
		if len(expectedVersions) == 0 {
			if len(infos) != 0 {
				t.Fatalf("metadata without a concrete plugin yielded packages: %+v", infos)
			}
			return
		}
		if readErr != nil {
			t.Fatalf("ReadInfo() error = %v", readErr)
		}
		if len(infos) != len(expectedVersions) {
			t.Fatalf(
				"ReadInfo() packages = %d, want %d: %+v",
				len(infos), len(expectedVersions), infos,
			)
		}
		byID := make(map[string]artifact.PackageInfo, len(infos))
		for _, info := range infos {
			id := string(info.Ref.Name)
			if _, expected := expectedVersions[id]; !expected {
				t.Fatalf("unexpected package identity %q", id)
			}
			if _, duplicate := byID[id]; duplicate {
				t.Fatalf("duplicate package identity %q", id)
			}
			byID[id] = info
		}

		if info, ok := byID[alphaID]; ok {
			checkSpongePackage(
				t, engine, baseConstraint, info,
				alphaID, expectedVersions[alphaID], "Alpha Display",
				globalDependencyID, optional,
			)
		}
		if info, ok := byID[betaID]; ok {
			checkSpongePackage(
				t, engine, baseConstraint, info,
				betaID, expectedVersions[betaID], "Beta Display",
				pluginDependencyID, optional,
			)
		}
	})
}

func checkSpongePackage(
	t *testing.T,
	engine version.Engine,
	baseConstraint version.Expr,
	info artifact.PackageInfo,
	id, expectedVersion, expectedTitle, expectedDependencyID string,
	optional bool,
) {
	t.Helper()
	if got := string(info.Ref.Name); got != id {
		t.Fatalf("package identity = %q, want %q", got, id)
	}
	if info.Ref.Version != version.Version(expectedVersion) {
		t.Fatalf("package %q version = %q, want %q", id, info.Ref.Version, expectedVersion)
	}
	if info.Ref.Ecosystem != catalog.EcosystemSponge {
		t.Fatalf("package %q ecosystem = %q, want sponge", id, info.Ref.Ecosystem)
	}
	if info.Ref.Provider != catalog.ProviderUnknown {
		t.Fatalf("package %q provider = %q, want unknown", id, info.Ref.Provider)
	}
	if info.Metadata.Title != expectedTitle {
		t.Fatalf("package %q title = %q, want %q", id, info.Metadata.Title, expectedTitle)
	}
	if info.Metadata.License != "MIT" {
		t.Fatalf("package %q license = %q, want MIT", id, info.Metadata.License)
	}

	if len(info.Dependencies) != 1 {
		t.Fatalf("package %q dependencies = %+v, want exactly one", id, info.Dependencies)
	}
	dependency := info.Dependencies[0]
	if got := string(dependency.Ref.Name); got != expectedDependencyID {
		t.Fatalf(
			"package %q dependency = %q, want %q (global/plugin dependency leaked)",
			id, got, expectedDependencyID,
		)
	}
	if dependency.Ref.Ecosystem != catalog.EcosystemSponge {
		t.Fatalf("package %q dependency ecosystem = %q, want sponge", id, dependency.Ref.Ecosystem)
	}
	if dependency.Mandatory != !optional {
		t.Fatalf("package %q dependency mandatory = %v, want %v", id, dependency.Mandatory, !optional)
	}
	for _, rawProbe := range []version.Version{"0.9.0", "1.0.0", "1.2.3", "1.5.0", "2.0.0", "3.0.0"} {
		probe, err := engine.ParseVersion(rawProbe)
		if err != nil {
			t.Fatalf("ParseVersion(%q) error = %v", rawProbe, err)
		}
		if got, want := engine.Match(dependency.Constraint, probe), engine.Match(baseConstraint, probe); got != want {
			t.Fatalf("package %q constraint match(%s) = %v, want %v", id, rawProbe, got, want)
		}
	}
}
