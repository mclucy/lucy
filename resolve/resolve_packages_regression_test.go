package resolve_test

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// canonicalAliasFixture publishes one project under both the reference the
// user writes and the provider's project identity: two release builds, the
// newer published most recently.
func canonicalAliasFixture(t *testing.T) *catalogueFixture {
	t.Helper()
	fixture := newCatalogueFixture(t)
	newest := stubVersionEntry("canonical", 0, "2.0.0", "release", true)
	older := stubVersionEntry("canonical", 1, "1.0.0", "release", true)
	fixture.addVersions(t, "alias-slug", []stubVersion{newest, older})
	fixture.addVersions(t, "canonical", []stubVersion{newest, older})
	fixture.addArchive(t, "canonical-f0", fabricArchive(t, fabricModuleSpec{ID: "canonical", Version: "2.0.0"}, nil))
	fixture.addArchive(t, "canonical-f1", fabricArchive(t, fabricModuleSpec{ID: "canonical", Version: "1.0.0"}, nil))
	return fixture
}

// canonicalAliasManifest binds one project through two references that
// canonicalize to the same selection.
func canonicalAliasManifest(slugSelector, identitySelector versionpkg.Version) *document.Manifest {
	return &document.Manifest{
		Server: document.ManifestServer{
			Minecraft: versionpkg.Version(fuzzMinecraft),
			Packages: map[catalog.Ecosystem]map[string]document.Requirement{
				catalog.EcosystemFabric: {
					"modrinth:alias-slug": {Selector: slugSelector},
					"modrinth:canonical":  {Selector: identitySelector},
				},
			},
		},
	}
}

// TestPackagesCanonicalAliasesConflictingSelectorsFail pins the failure side
// of merged requirements: two canonical aliases of one project select a single
// artifact, and when no candidate satisfies both enabled selectors the
// resolution fails instead of settling a graph with an unsatisfied
// requirement.
func TestPackagesCanonicalAliasesConflictingSelectorsFail(t *testing.T) {
	fixture := canonicalAliasFixture(t)

	out, warnings, err := resolve.Packages(resolve.PackagesRequest{
		Context:     t.Context(),
		Manifest:    canonicalAliasManifest("2.0.0", "1.0.0"),
		Target:      fuzzTarget,
		Candidates:  fixture.candidates,
		Materialize: fixture.materialize,
	})
	if err == nil {
		t.Fatal("Packages() succeeded although no candidate satisfies both selectors")
	}
	if out != nil {
		t.Fatalf("Packages() returned %d packages with an error", len(out))
	}
	if len(warnings) != 0 {
		t.Fatalf("Packages() returned %d warnings with an error", len(warnings))
	}
}

// TestPackagesCanonicalAliasesCompatibleSelectorsBacktrack pins the success
// side of the same rule: the newest candidate violates the selector merged by
// the second alias, so the search backtracks and settles on the older
// candidate that satisfies both.
func TestPackagesCanonicalAliasesCompatibleSelectorsBacktrack(t *testing.T) {
	fixture := canonicalAliasFixture(t)

	out, warnings, err := resolve.Packages(resolve.PackagesRequest{
		Context:     t.Context(),
		Manifest:    canonicalAliasManifest(versionpkg.Any, "1.0.0"),
		Target:      fuzzTarget,
		Candidates:  fixture.candidates,
		Materialize: fixture.materialize,
	})
	if err != nil {
		t.Fatalf("Packages() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("Packages() returned %d warnings for a valid graph", len(warnings))
	}
	if len(out) != 1 {
		t.Fatalf("Packages() returned %d packages, want one selection for both aliases", len(out))
	}
	selected := out[0]
	if selected.ProjectID != "canonical" {
		t.Fatalf("Packages() selected project %q, want the canonicalized identity", selected.ProjectID)
	}
	if selected.Version != "1.0.0" {
		t.Fatalf("Packages() selected %s, want the older candidate that satisfies both selectors", selected.Version)
	}
	for _, reference := range []string{"modrinth:alias-slug", "modrinth:canonical"} {
		if !slices.ContainsFunc(selected.Requirements, func(b document.RequirementBinding) bool {
			return b.Reference == reference
		}) {
			t.Fatalf("selection is missing the %s requirement binding", reference)
		}
	}
}

// TestPackagesWithOptionalDiscoversCataloguedOptionalDependency pins the
// WithOptional contract for an incomplete pin: a lock that adopted a release
// without dependency metadata cannot answer --with-optional alone, so the
// catalogue's optional dependency must still be discovered and selected.
func TestPackagesWithOptionalDiscoversCataloguedOptionalDependency(t *testing.T) {
	fixture := newCatalogueFixture(t)
	root := stubVersionEntry("root", 0, "1.0.0", "release", true)
	root.Dependencies = []stubDependency{{ProjectID: "extra", DependencyType: "optional"}}
	fixture.addVersions(t, "root", []stubVersion{root})
	fixture.addVersions(t, "extra", []stubVersion{stubVersionEntry("extra", 0, "1.0.0", "release", true)})
	fixture.addArchive(t, "root-f0", fabricArchive(t, fabricModuleSpec{ID: "root", Version: "1.0.0"}, nil))
	fixture.addArchive(t, "extra-f0", fabricArchive(t, fabricModuleSpec{ID: "extra", Version: "1.0.0"}, nil))

	// The lock adopted a release whose dependency metadata the provider could
	// not supply, so it records the pin without any declared relationship.
	previous := []document.Package{{
		Environment: serverpkg.EnvironmentServer,
		Ecosystem:   catalog.EcosystemFabric,
		Artifact: catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: "root",
			ReleaseID: "root-r0",
			FileID:    "root-f0",
			Version:   versionpkg.Version("1.0.0"),
			Filename:  "root-0.jar",
		},
		Channel:             upstream.ReleaseChannelRelease,
		DependenciesUnknown: true,
		Requirements: []document.RequirementBinding{{
			Reference: "modrinth:root",
			Selector:  versionpkg.Any,
			Enabled:   true,
		}},
	}}

	out, warnings, err := resolve.Packages(resolve.PackagesRequest{
		Context: t.Context(),
		Manifest: &document.Manifest{
			Server: document.ManifestServer{
				Minecraft: versionpkg.Version(fuzzMinecraft),
				Packages: map[catalog.Ecosystem]map[string]document.Requirement{
					catalog.EcosystemFabric: {
						"modrinth:root": {Selector: versionpkg.Any},
					},
				},
			},
		},
		Previous:           previous,
		PreserveLocked:     true,
		PreferLocked:       true,
		WithOptional:       true,
		ExpandDependencies: true,
		Target:             fuzzTarget,
		Candidates:         fixture.candidates,
		Materialize:        fixture.materialize,
	})
	if err != nil {
		t.Fatalf("Packages() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("Packages() returned %d warnings for a valid graph", len(warnings))
	}
	if !slices.ContainsFunc(out, func(p document.Package) bool { return p.ProjectID == "extra" }) {
		t.Fatal("Packages() did not discover the catalogued optional dependency")
	}
	if slices.ContainsFunc(out, func(p document.Package) bool {
		return p.ProjectID == "root" && p.DependenciesUnknown
	}) {
		t.Fatal("Packages() kept the incomplete pin metadata instead of the catalogue answer")
	}
}
