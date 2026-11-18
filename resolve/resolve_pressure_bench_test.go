package resolve_test

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/upstream"
)

// All three graphs use local Fabric jars so registry latency does not mask search work.
func BenchmarkPackageSearch(b *testing.B) {
	for _, shape := range []string{"dependencies", "locked-add", "duplicate"} {
		b.Run(shape, func(b *testing.B) {
			dir := b.TempDir()
			roots := map[string]document.Requirement{}
			builds := map[string][]upstream.Candidate{}
			count := 96
			if shape == "locked-add" {
				count = 465
			}
			if shape == "duplicate" {
				count = 11
			}
			for i := range count {
				name := fmt.Sprintf("pkg%04d", i)
				module := name
				if shape == "duplicate" && i == 1 {
					module = "pkg0000"
				}
				file, err := os.Create(filepath.Join(dir, name+".jar"))
				if err != nil {
					b.Fatal(err)
				}
				jar := zip.NewWriter(file)
				w, err := jar.Create("fabric.mod.json")
				if err != nil {
					b.Fatal(err)
				}
				if _, err := fmt.Fprintf(w, `{"schemaVersion":1,"id":%q,"version":"1.0.0","environment":"*"}`, module); err != nil {
					b.Fatal(err)
				}
				if err := jar.Close(); err != nil {
					b.Fatal(err)
				}
				if err := file.Close(); err != nil {
					b.Fatal(err)
				}
				c := upstream.Candidate{
					Artifact: catalog.Artifact{
						Provider:  catalog.ProviderModrinth,
						ProjectID: name,
						Version:   "1.0.0",
						ReleaseID: "r1",
						FileID:    name,
						Filename:  name + ".jar",
					},
					PrimaryFile: true,
					Channel:     upstream.ReleaseChannelRelease,
				}
				if shape != "dependencies" || i%2 == 0 {
					roots["modrinth:"+name] = document.Requirement{Selector: "any"}
					if shape == "dependencies" {
						c.Dependencies = []upstream.ProjectDependency{{
							Provider:  catalog.ProviderModrinth,
							ProjectID: fmt.Sprintf("pkg%04d", i+1),
							Kind:      catalog.DependencyKindRequired,
						}}
					}
				}
				builds[name] = []upstream.Candidate{c}
				if shape == "duplicate" && i > 1 {
					c.ReleaseID = "r2"
					c.Version = "2.0.0"
					builds[name] = append(builds[name], c)
				}
			}
			lookups, reads := 0, 0
			r := resolve.PackagesRequest{
				Context: b.Context(),
				Manifest: &document.Manifest{Server: document.ManifestServer{
					Packages: map[catalog.Ecosystem]map[string]document.Requirement{catalog.EcosystemFabric: roots},
				}},
				Target:             fuzzTarget,
				ExpandDependencies: true,
				Candidates: func(_ context.Context, _ catalog.Registry, name, _ string, _ catalog.Ecosystem) ([]upstream.Candidate, error) {
					lookups++
					return builds[name], nil
				},
				Materialize: func(a *catalog.Artifact) (artifact.MaterializedFile, error) {
					reads++
					return artifact.MaterializedFile{Path: filepath.Join(dir, a.Filename)}, nil
				},
			}
			if shape == "locked-add" {
				delete(roots, "modrinth:pkg0464")
				var err error
				r.Previous, _, err = resolve.Packages(r)
				if err != nil {
					b.Fatal(err)
				}
				r.PreserveLocked = true
				r.PreferLocked = true
				roots["modrinth:pkg0464"] = document.Requirement{Selector: "any"}
			}
			lookups, reads = 0, 0
			for b.Loop() {
				pkgs, _, err := resolve.Packages(r)
				if shape == "duplicate" {
					if err == nil || !strings.Contains(err.Error(), "duplicate native module pkg0000") {
						b.Fatalf("expected duplicate module rejection, got %v", err)
					}
					continue
				}
				if err != nil || len(pkgs) != count {
					b.Fatalf("resolved %d/%d: %v", len(pkgs), count, err)
				}
			}
			b.ReportMetric(float64(lookups)/float64(b.N), "lookups/op")
			b.ReportMetric(float64(reads)/float64(b.N), "jar-reads/op")
		})
	}
}
