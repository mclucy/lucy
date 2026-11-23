package install

import (
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
)

func TestStagePackagesReportsProgressDuringConcurrentFetches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "appdata"))
	remote := newAcquireTestRemote(t, []byte("package data"))
	const packageCount = 4
	locked := &document.Lock{Packages: make([]document.Package, packageCount)}
	for i := range locked.Packages {
		id := strconv.Itoa(i + 1)
		a := acquireTestSpigetArtifact(remote, id, "1.0.0")
		a.Filename = id + ".jar"
		locked.Packages[i] = document.Package{Artifact: *a, Ecosystem: catalog.EcosystemPaper}
	}

	active := make(map[string]bool)
	counts := make([]int, 0, packageCount)
	trace := &Trace{
		FetchStarted: func(name string, _ int64) { active[name] = true },
		FetchDone:    func(name string, _ bool) { delete(active, name) },
		PhaseProgress: func(_ string, count, total int) {
			if pending := len(active); pending > total-count {
				t.Errorf("%d transfers still active with only %d packages pending", pending, total-count)
			}
			counts = append(counts, count)
		},
	}
	store := &downloadStore{ctx: t.Context(), network: acquireTestCache(t), trace: trace}
	service := &Service{}
	if _, _, err := service.stagePackages(t.TempDir(), t.TempDir(), locked, store, nil, Options{Trace: trace}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(counts, []int{1, 2, 3, 4}) || len(active) != 0 {
		t.Fatalf("progress counts = %v, active transfers = %v", counts, active)
	}
	if got := remote.requests.Load(); got != packageCount {
		t.Fatalf("network downloads = %d, want %d", got, packageCount)
	}
}
