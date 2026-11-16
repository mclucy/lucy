package operation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// packageMapperTestSource answers the artifact-mapper and release lookups that
// package adoption consumes. Each provider identifies the file independently
// and lists its own releases, so a test controls which upstreams recognize the
// jar and which of them can select a release.
type packageMapperTestSource struct {
	initializeTestSource
	mappers  map[catalog.Provider]fakePackageMapper
	releases map[catalog.Provider][]upstream.Candidate

	// artifactURL and artifactSHA256 locate the jar every release points at.
	// initializePackageMapperLayout sets them after serving the jar.
	artifactURL    string
	artifactSHA256 string
}

func (s packageMapperTestSource) GetArtifactMapper(
	provider catalog.Provider,
) (upstream.ArtifactMapProvider, bool, error) {
	mapper, ok := s.mappers[provider]
	if !ok {
		return nil, false, nil
	}
	return mapper, true, nil
}

func (s packageMapperTestSource) Candidates(
	_ context.Context,
	registry catalog.Registry,
	_ string,
	_ string,
	_ catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	for provider, releases := range s.releases {
		if reg, _ := provider.Registry(); reg == registry {
			out := slices.Clone(releases)
			for i := range out {
				out[i].URL = s.artifactURL
				out[i].Hashes = map[string]string{"sha256": s.artifactSHA256}
			}
			return out, nil
		}
	}
	return nil, nil
}

// packageMapperInstallSource hands the installer's release lookups to the same
// fake catalogue the operation service uses. Resolution of the adopted lock
// runs through the installer, so without this its candidate list is empty.
type packageMapperInstallSource struct {
	initializeInstallSource
	catalogue packageMapperTestSource
}

func (s packageMapperInstallSource) Candidates(
	ctx context.Context,
	registry catalog.Registry,
	project, minecraft string,
	ecosystem catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	return s.catalogue.Candidates(ctx, registry, project, minecraft, ecosystem)
}

// fakePackageMapper answers a hash lookup the way an upstream does when it
// recognizes the file: as one project at one version. err makes the lookup
// fail the way an unreachable upstream does.
type fakePackageMapper struct {
	provider catalog.Provider
	name     catalog.Name
	version  versionpkg.Version
	err      error
}

func (m fakePackageMapper) Provider() catalog.Provider { return m.provider }

func (m fakePackageMapper) PackageByHash(
	context.Context,
	upstream.ArtifactFingerprint,
) (catalog.VersionedRef, string, bool, error) {
	if m.err != nil {
		return catalog.VersionedRef{}, "", false, m.err
	}
	return catalog.VersionedRef{
		Name:      m.name,
		Provider:  m.provider,
		Ecosystem: catalog.EcosystemFabric,
		Version:   m.version,
	}, "", true, nil
}

// releaseCandidate is one published release of a project, as the upstream
// catalogue lists it.
func releaseCandidate(
	provider catalog.Provider,
	projectID string,
	version string,
) upstream.Candidate {
	return upstream.Candidate{
		Artifact: catalog.Artifact{
			Provider:  provider,
			ProjectID: projectID,
			ReleaseID: "release-" + version,
			Version:   versionpkg.Version(version),
			Filename:  "example.jar",
		},
		PrimaryFile: true,
	}
}

// initializePackageMapperLayout initializes a fabric server holding one mod
// jar that every mapper examines, and returns the root with the written
// manifest and lock.
func initializePackageMapperLayout(
	t *testing.T,
	source packageMapperTestSource,
) (string, *documentpkg.Manifest, *documentpkg.Lock) {
	t.Helper()
	root, _ := initializePackageMapperResult(t, source)
	return root, readManifest(t, root), readLock(t, root)
}

// initializePackageMapperResult initializes the same layout and returns the
// whole result, so a test can inspect the warnings it records.
func initializePackageMapperResult(
	t *testing.T,
	source packageMapperTestSource,
) (string, *InitResult) {
	t.Helper()
	root := t.TempDir()
	writeFabricTestLauncher(t, root, "0.19.5", "1.21.1")
	modPath := filepath.Join(root, "mods", "example.jar")
	writeInitializeTestJar(t, modPath, map[string]string{
		"fabric.mod.json": `{"schemaVersion":1,"id":"example","version":"1.0.0"}`,
	})
	jar := readFileBytes(t, modPath)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(jar)
	}))
	t.Cleanup(server.Close)
	digest := sha256.Sum256(jar)
	source.artifactURL = server.URL + "/example.jar"
	source.artifactSHA256 = hex.EncodeToString(digest[:])
	svc := newInitializeTestServiceWithSource(t, source)
	network := cache.New(t.TempDir(), cache.DefaultCacheConfig(), http.DefaultClient, nil)
	svc.install = install.NewService(
		packageMapperInstallSource{catalogue: source},
		network,
		nil,
		nil,
		svc.registry,
		svc.logger,
		"",
	)
	result, err := svc.Initialize(
		t.Context(),
		root,
		InitOptions{},
	)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return root, result
}

// A file only CurseForge recognizes becomes managed under its
// provider-qualified reference and project identity.
func TestInitializeAdoptsCurseForgeOnlyMatch(t *testing.T) {
	_, manifest, lock := initializePackageMapperLayout(t, packageMapperTestSource{
		mappers: map[catalog.Provider]fakePackageMapper{
			catalog.ProviderCurseForge: {provider: catalog.ProviderCurseForge, name: "example", version: "1.0.0"},
		},
		releases: map[catalog.Provider][]upstream.Candidate{
			catalog.ProviderCurseForge: {releaseCandidate(catalog.ProviderCurseForge, "123", "1.0.0")},
		},
	})
	requirements := manifest.Server.Packages[catalog.EcosystemFabric]
	requirement, ok := requirements["curseforge:example"]
	if !ok {
		t.Fatalf("manifest packages = %v, want curseforge:example", requirements)
	}
	if got := requirement.Selector.String(); got != "1.0.0" {
		t.Fatalf("requirement selector = %q, want 1.0.0", got)
	}
	if len(lock.Packages) != 1 {
		t.Fatalf("lock packages = %d, want 1", len(lock.Packages))
	}
	if got := lock.Packages[0].Provider; got != catalog.ProviderCurseForge {
		t.Fatalf("lock provider = %q, want curseforge", got)
	}
	if got := lock.Packages[0].ProjectID; got != "123" {
		t.Fatalf("lock project ID = %q, want 123", got)
	}
}

// When both upstreams recognize the file, Modrinth is tried first and wins;
// CurseForge does not claim the same jar.
func TestInitializePrefersModrinthWhenBothMatch(t *testing.T) {
	_, manifest, lock := initializePackageMapperLayout(t, packageMapperTestSource{
		mappers: map[catalog.Provider]fakePackageMapper{
			catalog.ProviderModrinth:   {provider: catalog.ProviderModrinth, name: "example", version: "1.0.0"},
			catalog.ProviderCurseForge: {provider: catalog.ProviderCurseForge, name: "example", version: "1.0.0"},
		},
		releases: map[catalog.Provider][]upstream.Candidate{
			catalog.ProviderModrinth:   {releaseCandidate(catalog.ProviderModrinth, "abcd1234", "1.0.0")},
			catalog.ProviderCurseForge: {releaseCandidate(catalog.ProviderCurseForge, "123", "1.0.0")},
		},
	})
	requirements := manifest.Server.Packages[catalog.EcosystemFabric]
	if len(requirements) != 1 {
		t.Fatalf("manifest packages = %v, want only modrinth:example", requirements)
	}
	if _, ok := requirements["modrinth:example"]; !ok {
		t.Fatalf("manifest packages = %v, want modrinth:example", requirements)
	}
	if len(lock.Packages) != 1 {
		t.Fatalf("lock packages = %d, want 1", len(lock.Packages))
	}
	if got := lock.Packages[0].Provider; got != catalog.ProviderModrinth {
		t.Fatalf("lock provider = %q, want modrinth", got)
	}
	if got := lock.Packages[0].ProjectID; got != "abcd1234" {
		t.Fatalf("lock project ID = %q, want abcd1234", got)
	}
}

// A Modrinth match whose project offers no release for the hash-matched
// version is not a dead end: adoption falls through to CurseForge.
func TestInitializeFallsThroughToCurseForgeWithoutModrinthRelease(t *testing.T) {
	_, manifest, lock := initializePackageMapperLayout(t, packageMapperTestSource{
		mappers: map[catalog.Provider]fakePackageMapper{
			catalog.ProviderModrinth:   {provider: catalog.ProviderModrinth, name: "example", version: "1.0.0"},
			catalog.ProviderCurseForge: {provider: catalog.ProviderCurseForge, name: "example", version: "1.0.0"},
		},
		releases: map[catalog.Provider][]upstream.Candidate{
			catalog.ProviderCurseForge: {releaseCandidate(catalog.ProviderCurseForge, "123", "1.0.0")},
		},
	})
	requirements := manifest.Server.Packages[catalog.EcosystemFabric]
	if _, ok := requirements["curseforge:example"]; !ok {
		t.Fatalf("manifest packages = %v, want curseforge:example", requirements)
	}
	if _, ok := requirements["modrinth:example"]; ok {
		t.Fatalf("manifest packages = %v, Modrinth has no release to pin", requirements)
	}
	if len(lock.Packages) != 1 {
		t.Fatalf("lock packages = %d, want 1", len(lock.Packages))
	}
	if got := lock.Packages[0].Provider; got != catalog.ProviderCurseForge {
		t.Fatalf("lock provider = %q, want curseforge", got)
	}
}

// Without any configured mapper, an installed package stays unmanaged: the
// declaration gains no requirement and the file is left in place.
func TestInitializeLeavesPackageUnmanagedWithoutMapper(t *testing.T) {
	root, manifest, lock := initializePackageMapperLayout(
		t,
		packageMapperTestSource{},
	)
	if got := len(manifest.Server.Packages[catalog.EcosystemFabric]); got != 0 {
		t.Fatalf("manifest packages = %d, want none", got)
	}
	if got := len(lock.Packages); got != 0 {
		t.Fatalf("lock packages = %d, want none", got)
	}
	readFileBytes(t, filepath.Join(root, "mods", "example.jar"))
}

// A lookup error is not a miss: the file stays unmanaged, and the warning
// names the package, the provider, and the failure.
func TestInitializeWarnsWhenHashLookupFails(t *testing.T) {
	_, result := initializePackageMapperResult(t, packageMapperTestSource{
		mappers: map[catalog.Provider]fakePackageMapper{
			catalog.ProviderModrinth: {
				provider: catalog.ProviderModrinth,
				name:     "example",
				version:  "1.0.0",
				err:      errors.New("modrinth unavailable"),
			},
		},
	})
	if got := len(result.Warnings); got != 1 {
		t.Fatalf("warnings = %v, want one", result.Warnings)
	}
	message := result.Warnings[0].Error()
	for _, want := range []string{"example.jar", "modrinth", "modrinth unavailable"} {
		if !strings.Contains(message, want) {
			t.Errorf("warning %q does not name %q", message, want)
		}
	}
	if got := len(result.Document.Server.Packages[catalog.EcosystemFabric]); got != 0 {
		t.Fatalf("manifest packages = %d, want none", got)
	}
}
