package resolve_test

import (
	"fmt"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

// Rejected candidates must not leave dependency demand in the successful lock.
// Varying the parent count crosses slice capacity boundaries during backtracking.
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
		declarations := make(map[string]manifest.Requirement, count+1)
		for i := range count {
			name := fmt.Sprintf("m%02d", i)
			roots = append(roots, name)
			declarations["modrinth:"+name] = manifest.Requirement{Selector: version.Any}
			fixture.addVersions(t, name, []stubVersion{build(name, 0, "b", "")})
		}
		// This root is last, so three or more B demands precede A's candidate search.
		roots = append(roots, "zz-wrapper")
		declarations["modrinth:zz-wrapper"] = manifest.Requirement{Selector: version.Any}
		fixture.addVersions(t, "zz-wrapper", []stubVersion{build("zz-wrapper", 0, "a", "")})
		fixture.addVersions(t, "a", []stubVersion{
			build("a", 0, "b", "missing-module"),
			build("a", 1, "", ""),
		})
		fixture.addVersions(t, "b", []stubVersion{build("b", 0, "", "")})
		fixture.install(t)
		document := &manifest.Document{
			SchemaVersion: manifest.SchemaVersion,
			Server: manifest.Server{
				Directory: ".", Minecraft: fuzzMinecraft,
				Core:     manifest.Core{Product: server.ProductFabric, ProductVersion: fuzzLoaderVersion},
				Packages: map[catalog.Ecosystem]map[string]manifest.Requirement{catalog.EcosystemFabric: declarations},
			},
		}
		packages, warnings, err := resolve.Packages(resolve.PackagesRequest{
			Context: t.Context(), Manifest: document, Target: fuzzTarget,
			Materialize: fixture.materialize, ExpandDependencies: true,
		})
		if err != nil || len(warnings) != 0 {
			t.Fatalf("resolve graph with valid fallback: %v; warnings=%v", err, warnings)
		}
		if err := manifest.Write(fixture.dir, document); err != nil {
			t.Fatal(err)
		}
		fingerprint, err := manifest.Fingerprint(document)
		if err != nil {
			t.Fatal(err)
		}
		locked := &lockfile.Document{
			SchemaVersion: lockfile.SchemaVersion, ManifestFingerprint: fingerprint,
			Server: lockfile.Server{
				Directory: ".", Minecraft: fuzzMinecraft, Product: server.ProductFabric,
				CoreVersion: fuzzLoaderVersion, Version: fuzzLoaderVersion,
				Filename: "fabric.jar", Java: lockfile.Java{Advisory: 21},
			},
			Packages: packages,
		}
		if err := lockfile.Write(fixture.dir, locked); err != nil {
			t.Fatal(err)
		}
		replayed, err := lockfile.Read(fixture.dir)
		if err != nil {
			t.Fatal(err)
		}
		keep := roots[int(survivor)%len(roots)]
		document.Server.Packages[catalog.EcosystemFabric] = map[string]manifest.Requirement{
			"modrinth:" + keep: {Selector: version.Any},
		}
		plan, err := install.PlanInstall(t.Context(), fixture.dir, document, replayed, install.Options{
			Locked: true, RequirementChange: true,
		})
		if err != nil {
			t.Fatalf("plan locked removal: %v", err)
		}
		want := map[string]version.Version{keep: "1.0.0"}
		if keep == "zz-wrapper" {
			want["a"] = "2.0.0"
		} else {
			want["b"] = "1.0.0"
		}
		if len(plan.Resolved.Packages) != len(want) {
			t.Fatalf("keeping %q selected %+v, want %v", keep, plan.Resolved.Packages, want)
		}
		for _, pkg := range plan.Resolved.Packages {
			if pin, ok := want[pkg.ProjectID]; !ok || pin != pkg.Version || !pkg.Enabled() {
				t.Fatalf("keeping %q produced unexpected/inactive pin %+v, want %v", keep, pkg, want)
			}
			delete(want, pkg.ProjectID)
		}
	})
}
