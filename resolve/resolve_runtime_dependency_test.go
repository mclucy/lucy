package resolve_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
)

func TestDependencyAcrossLoadedEcosystems(t *testing.T) {
	for _, tc := range []struct {
		name           string
		provides       []string
		wantFabricFile bool
	}{
		{name: "distinct modules require both builds", wantFabricFile: true},
		{name: "loaded build covers dependency", provides: []string{"fabric_api"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCatalogueFixture(t)
			consumer := stubVersionEntry("consumer", 0, "1", "release", true)
			consumer.Dependencies = []stubDependency{{ProjectID: "shared", DependencyType: "required"}}
			fabric := stubVersionEntry("shared", 0, "1", "release", true)
			quilt := stubVersionEntry("shared", 1, "2", "release", true)
			quilt.Loaders = []string{"quilt"}
			f.addVersions(t, "consumer", []stubVersion{consumer})
			f.addVersions(t, "shared", []stubVersion{fabric, quilt})
			f.addArchive(t, consumer.Files[0].ID, fabricArchive(t, fabricModuleSpec{
				ID: "consumer", Version: "1", Depends: []string{"fabric_api"},
			}, nil))
			f.addArchive(t, fabric.Files[0].ID, fabricArchive(t, fabricModuleSpec{ID: "fabric_api", Version: "1"}, nil))
			f.addArchive(t, quilt.Files[0].ID, fabricArchive(t, fabricModuleSpec{
				ID: "quilt_api", Version: "2", Provides: tc.provides,
			}, nil))
			target := fuzzTarget
			target.Product = serverpkg.ProductQuilt
			pkgs, _, err := resolve.Packages(resolve.PackagesRequest{
				Manifest: &document.Manifest{Server: document.ManifestServer{
					Packages: map[catalog.Ecosystem]map[string]document.Requirement{
						catalog.EcosystemFabric: {"modrinth:consumer": {Selector: "any"}},
						catalog.EcosystemQuilt:  {"modrinth:shared": {Selector: "any"}},
					},
				}},
				Target:             target,
				Candidates:         f.candidates,
				Materialize:        f.materialize,
				ExpandDependencies: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := slices.ContainsFunc(pkgs, func(p document.Package) bool { return p.FileID == fabric.Files[0].ID })
			if got != tc.wantFabricFile {
				t.Fatalf("selected Fabric dependency build = %t, want %t", got, tc.wantFabricFile)
			}
		})
	}
}

func TestFailedSubstitutionsConsumeRetryBudget(t *testing.T) {
	f := newCatalogueFixture(t)
	consumer := stubVersionEntry("consumer", 0, "1", "release", true)
	roots := map[string]document.Requirement{}
	// Each edge can reuse the Quilt selection or install a Fabric build.
	// The last edge fails under every combination of those choices.
	for i := range 14 {
		project := fmt.Sprintf("shared%02d", i)
		roots["modrinth:"+project] = document.Requirement{Selector: "any"}
		consumer.Dependencies = append(consumer.Dependencies, stubDependency{ProjectID: project, DependencyType: "required"})
		fabric := stubVersionEntry(project, 0, "1", "release", true)
		quilt := stubVersionEntry(project, 1, "2", "release", true)
		quilt.Loaders = []string{"quilt"}
		f.addVersions(t, project, []stubVersion{fabric, quilt})
		for _, build := range []stubVersion{fabric, quilt} {
			f.addArchive(t, build.Files[0].ID, fabricArchive(t, fabricModuleSpec{ID: project, Version: build.VersionNumber}, nil))
		}
	}
	consumer.Dependencies = append(consumer.Dependencies, stubDependency{ProjectID: "missing", DependencyType: "required"})
	f.addVersions(t, "consumer", []stubVersion{consumer})
	f.addArchive(t, consumer.Files[0].ID, fabricArchive(t, fabricModuleSpec{ID: "consumer", Version: "1"}, nil))
	target := fuzzTarget
	target.Product = serverpkg.ProductQuilt
	_, _, err := resolve.Packages(resolve.PackagesRequest{
		Context: t.Context(),
		Manifest: &document.Manifest{Server: document.ManifestServer{
			Packages: map[catalog.Ecosystem]map[string]document.Requirement{
				catalog.EcosystemFabric: {"modrinth:consumer": {Selector: "any"}},
				catalog.EcosystemQuilt:  roots,
			},
		}},
		Target:             target,
		Candidates:         f.candidates,
		Materialize:        f.materialize,
		ExpandDependencies: true,
	})
	if _, ok := errors.AsType[*resolve.SearchLimitError](err); !ok {
		t.Fatalf("expected retry exhaustion, got %v", err)
	}
}
