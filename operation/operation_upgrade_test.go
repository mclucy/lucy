package operation

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

func TestParetoFrontierDropsDominatedOptions(t *testing.T) {
	options := []UpgradeOption{
		{Hash: "sha256:low", Metrics: UpgradeMetrics{UpgradedRoots: 1, VersionBumps: 1}},
		{Hash: "sha256:high-bumps", Metrics: UpgradeMetrics{UpgradedRoots: 2, VersionBumps: 5}},
		{Hash: "sha256:high-roots", Metrics: UpgradeMetrics{UpgradedRoots: 3, VersionBumps: 2}},
		{Hash: "sha256:dominated", Metrics: UpgradeMetrics{UpgradedRoots: 2, VersionBumps: 4}},
	}
	got := paretoFrontier(options)
	if len(got) != 2 {
		t.Fatalf("paretoFrontier() = %d options, want 2", len(got))
	}
	if got[0].Hash != "sha256:high-roots" || got[1].Hash != "sha256:high-bumps" {
		t.Fatalf("paretoFrontier() order = %q, %q", got[0].Hash, got[1].Hash)
	}
}

func TestSelectUpgradeOption(t *testing.T) {
	result := &UpgradeResult{Options: []UpgradeOption{
		{Hash: "sha256:more", Metrics: UpgradeMetrics{UpgradedRoots: 3, VersionBumps: 2}},
		{Hash: "sha256:later", Metrics: UpgradeMetrics{UpgradedRoots: 2, VersionBumps: 9}},
	}}
	more, err := SelectUpgradeOption(result, "more")
	if err != nil || more.Hash != "sha256:more" {
		t.Fatalf("SelectUpgradeOption(more) = %v, %v", more, err)
	}
	later, err := SelectUpgradeOption(result, "later")
	if err != nil || later.Hash != "sha256:later" {
		t.Fatalf("SelectUpgradeOption(later) = %v, %v", later, err)
	}
	hash, err := SelectUpgradeOption(result, "sha256:later")
	if err != nil || hash.Hash != "sha256:later" {
		t.Fatalf("SelectUpgradeOption(hash) = %v, %v", hash, err)
	}
	if _, err := SelectUpgradeOption(result, "sha256:missing"); err == nil {
		t.Fatal("SelectUpgradeOption(missing) error = nil, want a not-found error")
	}
}

func TestUpgradePlanHashIsOrderIndependent(t *testing.T) {
	packages := []lockfile.Package{
		{
			Environment: serverpkg.EnvironmentServer,
			Ecosystem:   catalog.EcosystemFabric,
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderModrinth,
				ProjectID: "a",
				Version:   versionpkg.Version("1.0.0"),
				ReleaseID: "r1",
				FileID:    "f1",
			},
		},
		{
			Environment: serverpkg.EnvironmentServer,
			Ecosystem:   catalog.EcosystemFabric,
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderModrinth,
				ProjectID: "b",
				Version:   versionpkg.Version("2.0.0"),
				ReleaseID: "r2",
				FileID:    "f2",
			},
		},
	}
	first := upgradePlanHash("manifest", "lock", "final", packages)
	reverse := []lockfile.Package{packages[1], packages[0]}
	second := upgradePlanHash("manifest", "lock", "final", reverse)
	if first != second {
		t.Fatalf("plan hash depends on package order: %q != %q", first, second)
	}
	packages[1].Version = "2.0.1"
	if third := upgradePlanHash("manifest", "lock", "final", packages); third == first {
		t.Fatal("plan hash did not change with a package version")
	}
}

func TestUpgradeCombinationsStayAtOrAboveLockedVersions(t *testing.T) {
	roots := []upgradeRoot{
		{LockedIndex: 2},
		{LockedIndex: 1},
	}
	combos := upgradeCombinations(roots, []int{0, 1}, 16)
	if len(combos) == 0 {
		t.Fatal("upgradeCombinations() returned no combinations")
	}
	for _, combo := range combos {
		if combo[0] > 2 || combo[1] > 1 {
			t.Fatalf("combination %v moved below a locked version", combo)
		}
	}
	if combos[0][0] != 0 || combos[0][1] != 0 {
		t.Fatalf("first combination = %v, want the newest versions", combos[0])
	}
}

func TestUpgradeBuildSelectorPrefersReleaseID(t *testing.T) {
	selector := upgradeBuildSelector(upstream.Candidate{
		Artifact: catalog.Artifact{Version: "1.2.3", ReleaseID: "release-id"},
	})
	if selector != "release-id" {
		t.Fatalf("upgradeBuildSelector() = %q, want the release id", selector)
	}
	selector = upgradeBuildSelector(upstream.Candidate{
		Artifact: catalog.Artifact{Version: "1.2.3"},
	})
	if selector != "1.2.3" {
		t.Fatalf("upgradeBuildSelector() = %q, want the version fallback", selector)
	}
}
