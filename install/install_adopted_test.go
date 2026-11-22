package install

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// adoptedTestIsolateHome keeps the per-user artifact store and configuration
// inside the test process.
func adoptedTestIsolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "appdata"))
}

func adoptedTestDigests(data []byte) map[string]string {
	sha256Sum := sha256.Sum256(data)
	sha1Sum := sha1.Sum(data)
	md5Sum := md5.Sum(data)
	return map[string]string{
		"sha256": hex.EncodeToString(sha256Sum[:]),
		"sha1":   hex.EncodeToString(sha1Sum[:]),
		"md5":    hex.EncodeToString(md5Sum[:]),
	}
}

func adoptedTestWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// A local runtime file answers several artifacts. Each materialization must
// return its own map: the store's SHA-256 plus only the digests that request
// pinned. A later request must never add a digest to a map an earlier caller
// already holds.
func TestLocalRuntimeSourceKeepsRequestedDigestsPerMaterialization(t *testing.T) {
	adoptedTestIsolateHome(t)
	root := t.TempDir()
	data := []byte("verified runtime bytes\n")
	path := filepath.Join(root, "server.jar")
	adoptedTestWrite(t, path, data)
	digests := adoptedTestDigests(data)
	source := newLocalRuntimeSource(root, []AdoptedRuntimeFile{{
		Path:           path,
		ExpectedSHA256: digests["sha256"],
	}})
	ctx := context.Background()

	first, matched, err := source.materialize(ctx, &catalog.Artifact{
		Filename: "server.jar",
		Hashes:   map[string]string{"sha256": digests["sha256"]},
	})
	if err != nil || !matched {
		t.Fatalf("sha256-only materialization = (%v, %v), want a match", matched, err)
	}
	if len(first.Hashes) != 1 || first.Hashes["sha256"] != digests["sha256"] {
		t.Fatalf("sha256-only hashes = %v, want only the store sha256", first.Hashes)
	}
	if got, err := os.ReadFile(first.Path); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("materialized bytes = %q, %v", got, err)
	}

	second, matched, err := source.materialize(ctx, &catalog.Artifact{
		Filename: "server.jar",
		Hashes:   map[string]string{"sha1": digests["sha1"]},
	})
	if err != nil || !matched {
		t.Fatalf("sha1 materialization = (%v, %v), want a match", matched, err)
	}
	if len(second.Hashes) != 2 ||
		second.Hashes["sha256"] != digests["sha256"] ||
		second.Hashes["sha1"] != digests["sha1"] {
		t.Fatalf("sha1 hashes = %v, want store sha256 and requested sha1", second.Hashes)
	}
	if _, ok := first.Hashes["sha1"]; ok {
		t.Fatalf("a later request added sha1 to an earlier map: %v", first.Hashes)
	}

	third, matched, err := source.materialize(ctx, &catalog.Artifact{
		Filename: "server.jar",
		Hashes:   map[string]string{"md5": digests["md5"]},
	})
	if err != nil || !matched {
		t.Fatalf("md5 materialization = (%v, %v), want a match", matched, err)
	}
	if len(third.Hashes) != 2 ||
		third.Hashes["sha256"] != digests["sha256"] ||
		third.Hashes["md5"] != digests["md5"] {
		t.Fatalf("md5 hashes = %v, want store sha256 and requested md5", third.Hashes)
	}
	if _, ok := second.Hashes["md5"]; ok {
		t.Fatalf("a later request added md5 to an earlier map: %v", second.Hashes)
	}
	if _, ok := third.Hashes["sha1"]; ok {
		t.Fatalf("unrequested sha1 leaked into hashes: %v", third.Hashes)
	}
}

// The adoption batch imports every accepted package through one artifact store
// while still verifying the observed SHA-256 and every digest the candidate
// pinned.
func TestPrepareAdoptedArtifactsVerifiesObservedAndRequestedDigests(t *testing.T) {
	adoptedTestIsolateHome(t)
	root := t.TempDir()
	alphaBytes := []byte("adopted alpha bytes\n")
	betaBytes := []byte("adopted beta bytes\n")
	alphaPath := filepath.Join(root, "mods", "alpha.jar")
	betaPath := filepath.Join(root, "mods", "beta.jar")
	adoptedTestWrite(t, alphaPath, alphaBytes)
	adoptedTestWrite(t, betaPath, betaBytes)
	alpha := adoptedTestDigests(alphaBytes)
	beta := adoptedTestDigests(betaBytes)
	release := func(id string, digests map[string]string) AdoptedRelease {
		return AdoptedRelease{
			Reference: "modrinth:" + id,
			Ecosystem: catalog.EcosystemFabric,
			Candidate: upstream.Candidate{Artifact: catalog.Artifact{
				Provider:  catalog.ProviderModrinth,
				ProjectID: id,
				Version:   versionpkg.Version("1.0.0"),
				Filename:  id + ".jar",
				Hashes:    map[string]string{"sha1": digests["sha1"]},
			}},
			Path:   "mods/" + id + ".jar",
			SHA256: digests["sha256"],
		}
	}
	releases := []AdoptedRelease{
		release("alpha", alpha),
		release("beta", beta),
	}
	cache, err := prepareAdoptedArtifacts(context.Background(), root, releases, nil)
	if err != nil {
		t.Fatalf("prepareAdoptedArtifacts() error = %v", err)
	}
	if cache != nil {
		t.Fatal("a replay-only batch built a resolution cache")
	}

	// Both packages must be reachable from the store by the observed SHA-256,
	// with no transfer cache and no injected materializer.
	store := &downloadStore{offline: true}
	for id, want := range map[string][]byte{"alpha": alphaBytes, "beta": betaBytes} {
		materialized, err := store.materializeFor(t.Context(), &catalog.Artifact{
			Filename: id + ".jar",
			Hashes:   map[string]string{"sha256": adoptedTestDigests(want)["sha256"]},
		}, id+".jar")
		if err != nil {
			t.Fatalf("materialize stored %s: %v", id, err)
		}
		if got, err := os.ReadFile(materialized.Path); err != nil || !bytes.Equal(got, want) {
			t.Fatalf("stored %s bytes = %q, %v", id, got, err)
		}
	}

	t.Run("candidate digest mismatch", func(t *testing.T) {
		broken := release("alpha", alpha)
		broken.Candidate.Hashes = map[string]string{"sha1": strings.Repeat("0", 40)}
		if _, err := prepareAdoptedArtifacts(
			context.Background(), root, []AdoptedRelease{broken}, nil,
		); err == nil {
			t.Fatal("a candidate digest mismatch was accepted")
		}
	})

	t.Run("observed digest mismatch", func(t *testing.T) {
		broken := release("alpha", alpha)
		broken.SHA256 = beta["sha256"]
		if _, err := prepareAdoptedArtifacts(
			context.Background(), root, []AdoptedRelease{broken}, nil,
		); err == nil {
			t.Fatal("an observed sha256 mismatch was accepted")
		}
	})
}

// Reusing a SHA-1-only lock must add the SHA-256 the observed runtime proved,
// keep every original pin, and leave the reused lock itself untouched. The
// resulting lock must then materialize the core from the artifact store with
// no injected materializer and no transfer cache.
func TestResolveReuseAddsProvenSHA256AndReplaysOffline(t *testing.T) {
	adoptedTestIsolateHome(t)
	root, manifest, lock := testWorkspace(t)
	holdWorkspaceLock(t, root)

	core := []byte("verified fabric core bytes\n")
	coreName := lock.Server.Artifact.Filename
	corePath := filepath.Join(root, filepath.FromSlash(coreName))
	adoptedTestWrite(t, corePath, core)
	digests := adoptedTestDigests(core)
	originalSHA1 := strings.ToUpper(digests["sha1"])
	lock.Server.Artifact.Hashes = map[string]string{"sha1": originalSHA1}

	resolved, result, err := testInstallService().Resolve(
		context.Background(),
		root,
		manifest,
		lock,
		Options{LocalRuntime: []AdoptedRuntimeFile{{
			Path:           corePath,
			ExpectedSHA256: digests["sha256"],
		}}},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.Mode != ModeReplay {
		t.Fatalf("Resolve() mode = %q, want %q", result.Mode, ModeReplay)
	}
	if resolved == lock {
		t.Fatal("adding a proven sha256 did not produce an enriched lock copy")
	}
	if got := resolved.Server.Artifact.Hashes["sha1"]; got != originalSHA1 {
		t.Fatalf("existing sha1 pin = %q, want the original %q", got, originalSHA1)
	}
	if got := resolved.Server.Artifact.Hashes["sha256"]; got != digests["sha256"] {
		t.Fatalf("proven sha256 = %q, want %q", got, digests["sha256"])
	}
	if _, ok := lock.Server.Artifact.Hashes["sha256"]; ok {
		t.Fatal("resolution mutated the reused lock")
	}

	// An offline replay has no injected materializer and an empty transfer
	// cache; only the proven store object can answer it.
	store := &downloadStore{offline: true}
	materialized, err := store.materializeFor(t.Context(), &resolved.Server.Artifact, "server core")
	if err != nil {
		t.Fatalf("materialize enriched core offline: %v", err)
	}
	if got, err := os.ReadFile(materialized.Path); err != nil || !bytes.Equal(got, core) {
		t.Fatalf("replayed core bytes = %q, %v", got, err)
	}
	if got := materialized.Hashes["sha256"]; got != digests["sha256"] {
		t.Fatalf("replayed sha256 = %q, want %q", got, digests["sha256"])
	}

	// A lock that already pins the proven SHA-256 must keep its exact bytes
	// and pointer identity.
	again, _, err := testInstallService().Resolve(
		context.Background(),
		root,
		manifest,
		resolved,
		Options{LocalRuntime: []AdoptedRuntimeFile{{
			Path:           corePath,
			ExpectedSHA256: digests["sha256"],
		}}},
	)
	if err != nil {
		t.Fatalf("Resolve() with unchanged pins error = %v", err)
	}
	if again != resolved {
		t.Fatal("an already-pinned lock no longer kept pointer identity")
	}
}
