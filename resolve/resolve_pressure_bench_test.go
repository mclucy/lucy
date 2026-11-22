package resolve_test

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/upstream"
)

// All four graphs use local Fabric jars so registry latency does not mask search work.
// Resolution overlaps upstream work with its search, so the counters that
// report its calls are atomic.
func BenchmarkPackageSearch(b *testing.B) {
	for _, shape := range []string{"dependencies", "locked-add", "duplicate", "duplicate-recovered"} {
		b.Run(shape, func(b *testing.B) {
			dir := b.TempDir()
			roots := map[string]document.Requirement{}
			builds := map[string][]upstream.Candidate{}
			count := 96
			if shape == "locked-add" {
				count = 465
			}
			if shape == "duplicate" || shape == "duplicate-recovered" {
				count = 11
			}
			writeJar := func(name, module string) {
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
			}
			for i := range count {
				name := fmt.Sprintf("pkg%04d", i)
				module := name
				if shape == "duplicate" && i == 1 {
					module = "pkg0000"
				}
				if shape == "duplicate-recovered" && (i == 0 || i == count-1) {
					// The first and last roots both claim the shared module;
					// only the first owner has a non-conflicting alternate.
					module = "shared"
				}
				writeJar(name, module)
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
				if (shape != "dependencies" || i%2 == 0) && !(shape == "locked-add" && (i == 462 || i == 464)) {
					roots["modrinth:"+name] = document.Requirement{Selector: "any"}
					if shape == "dependencies" {
						c.Dependencies = []upstream.ProjectDependency{{
							Provider:  catalog.ProviderModrinth,
							ProjectID: fmt.Sprintf("pkg%04d", i+1),
							Kind:      catalog.DependencyKindRequired,
						}}
					}
					if shape == "locked-add" && i == 463 {
						c.Dependencies = []upstream.ProjectDependency{{
							Provider:  catalog.ProviderModrinth,
							ProjectID: "pkg0462",
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
				if shape == "duplicate-recovered" && i >= 1 && i <= count-2 {
					// Unrelated roots keep alternates so recovery has to
					// search past them.
					c.ReleaseID = "r2"
					c.Version = "2.0.0"
					builds[name] = append(builds[name], c)
				}
				if shape == "duplicate-recovered" && i == 0 {
					c.ReleaseID = "r2"
					c.Version = "2.0.0"
					c.Filename = name + "-alt.jar"
					builds[name] = append(builds[name], c)
				}
			}
			if shape == "duplicate-recovered" {
				// The first owner's alternate installs its own module, so the
				// resolver can fall back to it after the shared-module
				// collision with the last root.
				writeJar("pkg0000-alt", "pkg0000")
			}
			var lookups, reads atomic.Int64
			r := resolve.PackagesRequest{
				Context: b.Context(),
				Manifest: &document.Manifest{Server: document.ManifestServer{
					Packages: map[catalog.Ecosystem]map[string]document.Requirement{catalog.EcosystemFabric: roots},
				}},
				Target:             fuzzTarget,
				ExpandDependencies: true,
				Candidates: func(_ context.Context, _ catalog.Registry, name, _ string, _ catalog.Ecosystem) ([]upstream.Candidate, error) {
					lookups.Add(1)
					return builds[name], nil
				},
				Materialize: func(_ context.Context, a *catalog.Artifact) (artifact.MaterializedFile, error) {
					reads.Add(1)
					return artifact.MaterializedFile{Path: filepath.Join(dir, a.Filename)}, nil
				},
			}
			if shape == "locked-add" {
				// pkg0462 enters the previous graph only through pkg0463's
				// dependency edge; pkg0464 is genuinely new and absent from
				// the lock until the addition below.
				var err error
				r.Previous, _, err = resolve.Packages(r)
				if err != nil {
					b.Fatal(err)
				}
				transitive := false
				for _, p := range r.Previous {
					if p.Artifact.FileID != "pkg0462" || !p.Dependency {
						continue
					}
					for _, dep := range p.DependencyOf {
						if dep.Provider == catalog.ProviderModrinth && dep.Name == "pkg0463" {
							transitive = true
						}
					}
				}
				if !transitive {
					b.Fatal("previous graph did not record pkg0462 as a transitive edge of pkg0463")
				}
				for _, p := range r.Previous {
					if p.Artifact.FileID == "pkg0464" {
						b.Fatal("pkg0464 already present in previous graph; fixture no longer adds a new package")
					}
				}
				r.PreserveLocked = true
				r.PreferLocked = true
				roots["modrinth:pkg0464"] = document.Requirement{Selector: "any"}
			}
			lookups.Store(0)
			reads.Store(0)
			for b.Loop() {
				pkgs, _, err := resolve.Packages(r)
				switch shape {
				case "duplicate":
					if err == nil || !strings.Contains(err.Error(), "duplicate native module pkg0000") {
						b.Fatalf("expected duplicate module rejection, got %v", err)
					}
					continue
				case "duplicate-recovered":
					if err != nil {
						b.Fatalf("expected the alternate graph to settle, got %v", err)
					}
					if len(pkgs) != count {
						b.Fatalf("resolved %d/%d: %v", len(pkgs), count, err)
					}
					for _, p := range pkgs {
						if p.Artifact.FileID == "pkg0000" && p.Artifact.Version != "2.0.0" {
							b.Fatalf("first owner pkg0000 settled on %s, want the alternate 2.0.0", p.Artifact.Version)
						}
						if p.Artifact.FileID == "pkg0010" && p.Artifact.Version != "1.0.0" {
							b.Fatalf("last owner pkg0010 settled on %s, want its primary 1.0.0", p.Artifact.Version)
						}
					}
					continue
				}
				if err != nil || len(pkgs) != count {
					b.Fatalf("resolved %d/%d: %v", len(pkgs), count, err)
				}
				if shape == "locked-add" {
					// The addition must not disturb the recorded transitive
					// edge: pkg0462 stays a dependency of pkg0463.
					transitive := false
					for _, p := range pkgs {
						if p.Artifact.FileID != "pkg0462" || !p.Dependency {
							continue
						}
						for _, dep := range p.DependencyOf {
							if dep.Provider == catalog.ProviderModrinth && dep.Name == "pkg0463" {
								transitive = true
							}
						}
					}
					if !transitive {
						b.Fatal("resolved graph dropped pkg0462's transitive edge from pkg0463")
					}
				}
			}
			b.ReportMetric(float64(lookups.Load())/float64(b.N), "lookups/op")
			b.ReportMetric(float64(reads.Load())/float64(b.N), "jar-reads/op")
		})
	}
}
