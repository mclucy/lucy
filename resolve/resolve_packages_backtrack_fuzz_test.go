package resolve_test

import (
	"fmt"
	"testing"

	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

// FuzzPackagesBacktrackDemand checks that resolution retains only the dependency
// closure of current declarations. Varying the parent count crosses slice
// capacity boundaries during backtracking.
func FuzzPackagesBacktrackDemand(f *testing.F) {
	f.Add(uint8(0), uint8(2))
	f.Add(uint8(0), uint8(3))
	f.Add(uint8(1), uint8(4))
	f.Add(uint8(2), uint8(4))
	f.Fuzz(func(t *testing.T, parentCount, survivor uint8) {
		count := 3 + int(parentCount%5)
		fixture := newCatalogueFixture(t)
		build := func(project string, index int, dependency, missingModule string) stubVersion {
			entry := stubVersionEntry(project, index, selectionCandidateVersion(index), "release", true)
			if dependency != "" {
				entry.Dependencies = []stubDependency{{ProjectID: dependency, DependencyType: "required"}}
			}
			module := fabricModuleSpec{ID: project, Version: entry.VersionNumber}
			if missingModule != "" {
				module.Depends = []string{missingModule}
			}
			fixture.addArchive(t, entry.Files[0].ID, fabricArchive(t, module, nil))
			return entry
		}
		roots := make([]string, 0, count+1)
		declarations := make(map[string]documentpkg.Requirement, count+1)
		for i := range count {
			name := fmt.Sprintf("m%02d", i)
			roots = append(roots, name)
			declarations["modrinth:"+name] = documentpkg.Requirement{Selector: version.Any}
			fixture.addVersions(t, name, []stubVersion{build(name, 0, "b", "")})
		}
		// This root is last, so three or more B demands precede A's candidate search.
		roots = append(roots, "zz-wrapper")
		declarations["modrinth:zz-wrapper"] = documentpkg.Requirement{Selector: version.Any}
		fixture.addVersions(t, "zz-wrapper", []stubVersion{build("zz-wrapper", 0, "a", "")})
		fixture.addVersions(t, "a", []stubVersion{
			build("a", 0, "b", "missing-module"),
			build("a", 1, "", ""),
		})
		fixture.addVersions(t, "b", []stubVersion{build("b", 0, "", "")})
		document := &documentpkg.Manifest{
			SchemaVersion: documentpkg.ManifestSchemaVersion,
			Server: documentpkg.ManifestServer{
				Directory: ".", Minecraft: fuzzMinecraft,
				Core:     documentpkg.ManifestCore{Product: server.ProductFabric, ProductVersion: fuzzLoaderVersion},
				Packages: map[catalog.Ecosystem]map[string]documentpkg.Requirement{catalog.EcosystemFabric: declarations},
			},
		}
		packages, warnings, err := resolve.Packages(resolve.PackagesRequest{
			Context: t.Context(), Manifest: document, Target: fuzzTarget,
			Candidates:  fixture.candidates,
			Materialize: fixture.materialize, ExpandDependencies: true,
		})
		if err != nil || len(warnings) != 0 {
			t.Fatalf("resolve graph with valid fallback: %v; warnings=%v", err, warnings)
		}
		wantFull := map[string]version.Version{"a": "2.0.0", "b": "1.0.0"}
		for _, root := range roots {
			wantFull[root] = "1.0.0"
		}
		checkResolvedPackages(t, packages, wantFull, "full graph")
		keep := roots[int(survivor)%len(roots)]
		document.Server.Packages[catalog.EcosystemFabric] = map[string]documentpkg.Requirement{
			"modrinth:" + keep: {Selector: version.Any},
		}
		reduced, warnings, err := resolve.Packages(resolve.PackagesRequest{
			Context: t.Context(), Manifest: document, Target: fuzzTarget,
			Candidates:  fixture.candidates,
			Materialize: fixture.materialize, ExpandDependencies: true,
		})
		if err != nil || len(warnings) != 0 {
			t.Fatalf("resolve reduced graph keeping %q: %v; warnings=%v", keep, err, warnings)
		}
		want := map[string]version.Version{keep: "1.0.0"}
		if keep == "zz-wrapper" {
			want["a"] = "2.0.0"
		} else {
			want["b"] = "1.0.0"
		}
		checkResolvedPackages(t, reduced, want, fmt.Sprintf("keeping %q", keep))
	})
}

func checkResolvedPackages(
	t *testing.T,
	packages []documentpkg.Package,
	want map[string]version.Version,
	description string,
) {
	t.Helper()
	if len(packages) != len(want) {
		t.Fatalf("%s selected %+v, want %v", description, packages, want)
	}
	for _, pkg := range packages {
		if pin, ok := want[pkg.ProjectID]; !ok || pin != pkg.Version || !pkg.Enabled() {
			t.Fatalf("%s produced unexpected/inactive pin %+v, want %v", description, pkg, want)
		}
		delete(want, pkg.ProjectID)
	}
}
