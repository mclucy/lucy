package artifact_test

import (
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/version"
)

// quiltDependencyFixture models a Quilt dependency object. The Quilt schema
// accepts a bare mod ID in the depends/breaks arrays as well, so fixtures mix
// strings and objects.
type quiltDependencyFixture struct {
	ID       string `json:"id"`
	Versions any    `json:"versions,omitempty"`
	Optional bool   `json:"optional,omitempty"`
}

type quiltMetadataFixture struct {
	Name         string            `json:"name,omitempty"`
	Description  string            `json:"description,omitempty"`
	Contributors map[string]any    `json:"contributors,omitempty"`
	Contact      map[string]string `json:"contact,omitempty"`
	License      any               `json:"license,omitempty"`
}

type quiltLoaderFixture struct {
	Group    string                `json:"group"`
	ID       string                `json:"id"`
	Version  string                `json:"version"`
	Depends  []any                 `json:"depends,omitempty"`
	Breaks   []any                 `json:"breaks,omitempty"`
	Metadata *quiltMetadataFixture `json:"metadata,omitempty"`
}

type quiltDescriptorFixture struct {
	SchemaVersion int                `json:"schema_version"`
	QuiltLoader   quiltLoaderFixture `json:"quilt_loader"`
}

func quiltDescriptor(t *testing.T, loader quiltLoaderFixture) []byte {
	t.Helper()
	return jsonFixture(
		t,
		quiltDescriptorFixture{SchemaVersion: 1, QuiltLoader: loader},
	)
}

func writeQuiltArtifact(t *testing.T, entries ...zipFixtureEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.jar")
	writeZipFixture(t, path, entries...)
	return path
}

func quiltPackage(t *testing.T, path string) artifact.PackageInfo {
	t.Helper()
	infos, err := artifact.ReadInfo(path)
	if err != nil {
		t.Fatalf("ReadInfo() error = %v", err)
	}
	infos = artifact.PreferredPackageInfos(
		infos,
		catalog.EcosystemUnspecified,
	)
	if len(infos) != 1 {
		t.Fatalf("ReadInfo() packages = %d, want 1: %+v", len(infos), infos)
	}
	return infos[0]
}

func quiltDependency(
	t *testing.T,
	dependencies []artifact.Dependency,
	name string,
) artifact.Dependency {
	t.Helper()
	for _, dependency := range dependencies {
		if string(dependency.Ref.Name) == name {
			return dependency
		}
	}
	t.Fatalf("dependency %q not found in %+v", name, dependencies)
	return artifact.Dependency{}
}

// quiltConstraintEngine binds the matcher to the grammar the reader actually
// parsed. Quilt and Fabric may share a dialect, and a distinct Quilt dialect
// must still match its own parsed bounds, so the dialect comes from the
// translated expression rather than a fixed choice.
func quiltConstraintEngine(t *testing.T, expr version.Expr) version.Engine {
	t.Helper()
	for _, conjunction := range expr {
		for _, term := range conjunction {
			if term.Value == nil {
				continue
			}
			engine, err := version.NewEngine(term.Value.Dialect())
			if err != nil {
				t.Fatalf("NewEngine(%q) error = %v", term.Value.Dialect(), err)
			}
			return engine
		}
	}
	engine, err := version.NewEngine(version.DialectFabric)
	if err != nil {
		t.Fatalf("NewEngine(fabric) error = %v", err)
	}
	return engine
}

// assertQuiltConstraint checks policy-visible matching rather than expression
// internals, so compound expressions translate consistently.
func assertQuiltConstraint(
	t *testing.T,
	expr version.Expr,
	matches map[string]bool,
) {
	t.Helper()
	engine := quiltConstraintEngine(t, expr)
	for raw, want := range matches {
		probe, err := engine.ParseVersion(version.Version(raw))
		if err != nil {
			t.Fatalf("ParseVersion(%q) error = %v", raw, err)
		}
		if got := engine.Match(expr, probe); got != want {
			t.Fatalf("constraint match(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestReadInfoQuiltIdentityAndMetadata(t *testing.T) {
	t.Parallel()
	path := writeQuiltArtifact(t, zipFixtureEntry{
		name: "quilt.mod.json",
		data: quiltDescriptor(t, quiltLoaderFixture{
			Group:   "org.quiltmc",
			ID:      "quilt_example_mod",
			Version: "1.2.3",
			Metadata: &quiltMetadataFixture{
				Name:        "Quilt Example Mod",
				Description: "An example mod for the Quilt ecosystem.",
				Contributors: map[string]any{
					"Haven King": "Developer",
					"QuiltMC":    []string{"Owner", "Developer"},
				},
				Contact: map[string]string{
					"homepage": "https://quiltmc.org/",
					"sources":  "https://github.com/QuiltMC/quilt-example-mod",
					"issues":   "https://github.com/QuiltMC/quilt-example-mod/issues",
				},
				License: "CC0-1.0",
			},
		}),
	})

	info := quiltPackage(t, path)
	if got, want := string(info.Ref.Name), "quilt-example-mod"; got != want {
		t.Fatalf("package identity = %q, want %q", got, want)
	}
	if info.Ref.Ecosystem != catalog.EcosystemQuilt {
		t.Fatalf("package ecosystem = %q, want quilt", info.Ref.Ecosystem)
	}
	if info.Ref.Provider != catalog.ProviderUnknown {
		t.Fatalf("package provider = %q, want unknown", info.Ref.Provider)
	}
	if info.Ref.Version != version.Version("1.2.3") {
		t.Fatalf("package version = %q, want 1.2.3", info.Ref.Version)
	}
	if info.FilePath != path {
		t.Fatalf("package path = %q, want %q", info.FilePath, path)
	}
	if info.Metadata.Title != "Quilt Example Mod" {
		t.Fatalf("metadata title = %q, want %q", info.Metadata.Title, "Quilt Example Mod")
	}
	if info.Metadata.Description != "An example mod for the Quilt ecosystem." {
		t.Fatalf("metadata description = %q", info.Metadata.Description)
	}
	if info.Metadata.License != "CC0-1.0" {
		t.Fatalf("metadata license = %q, want CC0-1.0", info.Metadata.License)
	}

	authors := make(map[string]struct{}, len(info.Metadata.Authors))
	for _, author := range info.Metadata.Authors {
		authors[author.Name] = struct{}{}
	}
	for _, want := range []string{"Haven King", "QuiltMC"} {
		if _, ok := authors[want]; !ok {
			t.Fatalf("metadata authors = %+v, missing %q", info.Metadata.Authors, want)
		}
	}
	for name := range authors {
		if name != "Haven King" && name != "QuiltMC" {
			t.Fatalf("metadata authors = %+v, unexpected %q", info.Metadata.Authors, name)
		}
	}

	urls := make(map[catalog.URLType]string, len(info.Metadata.URLs))
	for _, url := range info.Metadata.URLs {
		urls[url.Type] = url.URL
	}
	for urlType, want := range map[catalog.URLType]string{
		catalog.URLHome:   "https://quiltmc.org/",
		catalog.URLSource: "https://github.com/QuiltMC/quilt-example-mod",
		catalog.URLIssues: "https://github.com/QuiltMC/quilt-example-mod/issues",
	} {
		if got := urls[urlType]; got != want {
			t.Fatalf("metadata URL %s = %q, want %q", urlType, got, want)
		}
	}
}

func TestReadInfoQuiltDependencyTranslation(t *testing.T) {
	t.Parallel()
	path := writeQuiltArtifact(t, zipFixtureEntry{
		name: "quilt.mod.json",
		data: quiltDescriptor(t, quiltLoaderFixture{
			Group:   "org.quiltmc",
			ID:      "quilt_dependency_mod",
			Version: "2.0.0",
			Depends: []any{
				"quilt_networking_api",
				quiltDependencyFixture{
					ID:       "quilt_rendering_api",
					Versions: ">=1.0.0 <2.0.0",
				},
				quiltDependencyFixture{
					ID:       "sodium",
					Versions: "~0.5.0",
					Optional: true,
				},
			},
			Breaks: []any{
				quiltDependencyFixture{
					ID:       "old_lib",
					Versions: "<1.0.0",
				},
				"conflicting_mod",
			},
		}),
	})

	info := quiltPackage(t, path)
	if len(info.Dependencies) != 5 {
		t.Fatalf(
			"package dependencies = %d, want 5: %+v",
			len(info.Dependencies), info.Dependencies,
		)
	}

	required := quiltDependency(t, info.Dependencies, "quilt-networking-api")
	if required.Kind != catalog.DependencyKindRequired {
		t.Fatalf("dependency kind = %q, want required", required.Kind)
	}
	if !required.Mandatory {
		t.Fatalf("bare-string dependency is not mandatory")
	}
	if required.Origin != catalog.DependencyRegular {
		t.Fatalf("dependency origin = %q, want regular", required.Origin)
	}
	if required.Ref.Ecosystem != catalog.EcosystemQuilt {
		t.Fatalf("dependency ecosystem = %q, want quilt", required.Ref.Ecosystem)
	}
	if required.Ref.Provider != catalog.ProviderUnknown {
		t.Fatalf("dependency provider = %q, want unknown", required.Ref.Provider)
	}
	assertQuiltConstraint(t, required.Constraint, map[string]bool{
		"0.0.0": true,
		"1.2.3": true,
		"2.0.0": true,
	})

	rendering := quiltDependency(t, info.Dependencies, "quilt-rendering-api")
	if rendering.Kind != catalog.DependencyKindRequired {
		t.Fatalf("dependency kind = %q, want required", rendering.Kind)
	}
	if !rendering.Mandatory {
		t.Fatalf("object dependency is not mandatory")
	}
	assertQuiltConstraint(t, rendering.Constraint, map[string]bool{
		"0.5.0": false,
		"1.0.0": true,
		"1.2.3": true,
		"2.0.0": false,
	})

	optional := quiltDependency(t, info.Dependencies, "sodium")
	if optional.Kind != catalog.DependencyKindOptional {
		t.Fatalf("optional dependency kind = %q, want optional", optional.Kind)
	}
	if optional.Mandatory {
		t.Fatalf("optional dependency is mandatory")
	}
	assertQuiltConstraint(t, optional.Constraint, map[string]bool{
		"0.5.2": true,
		"0.6.0": false,
		"1.0.0": false,
	})

	incompatible := quiltDependency(t, info.Dependencies, "old-lib")
	if incompatible.Kind != catalog.DependencyKindIncompatible {
		t.Fatalf("break kind = %q, want incompatible", incompatible.Kind)
	}
	if incompatible.Mandatory {
		t.Fatalf("break is mandatory")
	}
	assertQuiltConstraint(t, incompatible.Constraint, map[string]bool{
		"0.5.2": false,
		"1.0.0": true,
		"2.0.0": true,
	})

	wildcardBreak := quiltDependency(t, info.Dependencies, "conflicting-mod")
	if wildcardBreak.Kind != catalog.DependencyKindIncompatible {
		t.Fatalf("break kind = %q, want incompatible", wildcardBreak.Kind)
	}
	assertQuiltConstraint(t, wildcardBreak.Constraint, map[string]bool{
		"0.0.0": false,
		"1.2.3": false,
		"2.0.0": false,
	})
}

func TestReadInfoQuiltCompoundConstraints(t *testing.T) {
	t.Parallel()
	path := writeQuiltArtifact(t, zipFixtureEntry{
		name: "quilt.mod.json",
		data: quiltDescriptor(t, quiltLoaderFixture{
			Group:   "org.quiltmc",
			ID:      "quilt_compound_mod",
			Version: "1.0.0",
			Depends: []any{
				quiltDependencyFixture{
					ID: "any_range",
					Versions: map[string]any{
						"any": []string{">=1.0.0", "<0.5.0"},
					},
				},
				quiltDependencyFixture{
					ID: "all_range",
					Versions: map[string]any{
						"all": []string{">=1.0.0", "<2.0.0"},
					},
				},
			},
		}),
	})

	info := quiltPackage(t, path)
	if len(info.Dependencies) != 2 {
		t.Fatalf(
			"package dependencies = %d, want 2: %+v",
			len(info.Dependencies), info.Dependencies,
		)
	}
	anyRange := quiltDependency(t, info.Dependencies, "any-range")
	assertQuiltConstraint(t, anyRange.Constraint, map[string]bool{
		"0.1.0": true,
		"0.7.0": false,
		"1.2.3": true,
		"2.0.0": true,
	})

	allRange := quiltDependency(t, info.Dependencies, "all-range")
	assertQuiltConstraint(t, allRange.Constraint, map[string]bool{
		"0.5.0": false,
		"1.0.0": true,
		"1.5.0": true,
		"2.0.0": false,
	})
}

func TestReadInfoQuiltBareVersionSpecifier(t *testing.T) {
	t.Parallel()
	path := writeQuiltArtifact(t, zipFixtureEntry{
		name: "quilt.mod.json",
		data: quiltDescriptor(t, quiltLoaderFixture{
			Group:   "org.quiltmc",
			ID:      "quilt_bare_version_mod",
			Version: "1.0.0",
			Depends: []any{
				quiltDependencyFixture{
					ID:       "bare_range",
					Versions: "1.0.0",
				},
				quiltDependencyFixture{
					ID:       "exact_range",
					Versions: "=1.0.0",
				},
			},
		}),
	})

	info := quiltPackage(t, path)
	bare := quiltDependency(t, info.Dependencies, "bare-range")
	assertQuiltConstraint(t, bare.Constraint, map[string]bool{
		"0.9.0": false,
		"1.0.0": true,
		"1.9.9": true,
		"2.0.0": false,
	})

	exact := quiltDependency(t, info.Dependencies, "exact-range")
	assertQuiltConstraint(t, exact.Constraint, map[string]bool{
		"1.0.0": true,
		"1.0.1": false,
		"2.0.0": false,
	})
}

func TestReadInfoQuiltTakesPrecedenceOverFabric(t *testing.T) {
	t.Parallel()
	path := writeQuiltArtifact(
		t,
		zipFixtureEntry{
			name: "quilt.mod.json",
			data: quiltDescriptor(t, quiltLoaderFixture{
				Group:   "org.quiltmc",
				ID:      "quilt_first",
				Version: "1.2.3",
				Metadata: &quiltMetadataFixture{
					Name: "Quilt Descriptor",
				},
			}),
		},
		zipFixtureEntry{
			name: "fabric.mod.json",
			data: []byte(`{
				"id": "fabric_fallback",
				"version": "9.9.9",
				"name": "Fabric Descriptor"
			}`),
		},
	)

	info := quiltPackage(t, path)
	if info.Ref.Ecosystem != catalog.EcosystemQuilt {
		t.Fatalf("package ecosystem = %q, want quilt", info.Ref.Ecosystem)
	}
	if got, want := string(info.Ref.Name), "quilt-first"; got != want {
		t.Fatalf("package identity = %q, want %q", got, want)
	}
	if info.Ref.Version != version.Version("1.2.3") {
		t.Fatalf("package version = %q, want 1.2.3", info.Ref.Version)
	}
	if info.Metadata.Title != "Quilt Descriptor" {
		t.Fatalf("metadata title = %q, want Quilt Descriptor", info.Metadata.Title)
	}
}
