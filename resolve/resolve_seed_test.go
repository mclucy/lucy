package resolve

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// A package that lost a bundled module to another package keeps only the
// surviving modules in the lock. Seeding the analysis cache from that list
// would hide the lost module from every later search.
func TestSeedPreviousSkipsPackagesWithSupersededModules(t *testing.T) {
	target := Target{
		Product:     serverpkg.ProductFabric,
		Minecraft:   versionpkg.Version("1.21.1"),
		CoreVersion: versionpkg.Version("0.16.0"),
	}
	bundled := quiltTestModule("shared", "1.0.0", catalog.EcosystemFabric)
	bundled.NestedPath = "META-INF/jars/shared.jar"
	packages := []document.Package{
		quiltTestPackage(
			catalog.EcosystemFabric,
			"host",
			quiltTestModule("host", "1.0.0", catalog.EcosystemFabric),
			bundled,
		),
		quiltTestPackage(
			catalog.EcosystemFabric,
			"standalone",
			quiltTestModule("shared", "1.0.0", catalog.EcosystemFabric),
		),
	}
	for i := range packages {
		packages[i].Hashes = map[string]string{"sha256": packages[i].ProjectID}
	}
	if _, err := finalizeModules(packages, target); err != nil {
		t.Fatalf("finalizeModules() error = %v", err)
	}
	if !packages[0].ModulesSuperseded || packages[1].ModulesSuperseded {
		t.Fatalf(
			"ModulesSuperseded = host %v, standalone %v; want true, false",
			packages[0].ModulesSuperseded,
			packages[1].ModulesSuperseded,
		)
	}

	cache := NewAnalysisCache()
	cache.SeedPrevious(packages, target, target)

	for _, p := range packages {
		key := analysisKeyFor(
			upstream.Candidate{Artifact: p.Artifact},
			target.DescriptorLoader(p.Ecosystem),
			target.Minecraft,
		)
		if got, want := cache.has(key, p.Hashes), !p.ModulesSuperseded; got != want {
			t.Errorf("seeded %s = %v, want %v", p.ProjectID, got, want)
		}
	}
}
