package install

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/upstream"
)

// AdoptedRelease pairs a release the workspace already holds with the manifest
// reference that declares it.
type AdoptedRelease struct {
	Reference string
	Ecosystem catalog.Ecosystem
	Candidate upstream.Candidate
	Path      string
	SHA256    string
	Analysis  *artifact.Analysis
}

// adoptedCandidate answers a catalogue lookup from a release the workspace
// already holds. Adoption declares the installed version, so no catalogue
// query can select different bytes for it. The reference answers the project
// name as written; the project identity answers a demand that names it.
func adoptedCandidate(
	adopted []AdoptedRelease,
	registry catalog.Registry,
	project string,
	ecosystem catalog.Ecosystem,
) (upstream.Candidate, bool) {
	for _, release := range adopted {
		if release.Ecosystem != ecosystem {
			continue
		}
		if releaseRegistry, ok := release.Candidate.Provider.Registry(); !ok ||
			releaseRegistry != registry {
			continue
		}
		provider, name, err := document.ParseReference(release.Reference)
		if err != nil || provider != release.Candidate.Provider {
			continue
		}
		if name == project || release.Candidate.ProjectID == project {
			candidate := release.Candidate
			candidate.PrimaryFile = true
			return candidate, true
		}
	}
	return upstream.Candidate{}, false
}

func supersededByAdoption(pkg document.Package, adopted []AdoptedRelease) bool {
	for _, release := range adopted {
		if release.Ecosystem != pkg.Ecosystem || release.Candidate.Provider != pkg.Provider {
			continue
		}
		if release.Candidate.ProjectID == pkg.ProjectID {
			return true
		}
		for _, req := range pkg.Requirements {
			if req.Reference == release.Reference {
				return true
			}
		}
	}
	return false
}

func adoptionMatchesLock(adopted []AdoptedRelease, lock *document.Lock) bool {
	for _, release := range adopted {
		candidate := release.Candidate
		matches := false
		for _, pkg := range lock.Packages {
			if pkg.Ecosystem == release.Ecosystem && pkg.Provider == candidate.Provider &&
				pkg.ProjectID == candidate.ProjectID && pkg.ReleaseID == candidate.ReleaseID &&
				pkg.FileID == candidate.FileID && pkg.Filename == candidate.Filename &&
				strings.EqualFold(pkg.Hashes["sha256"], release.SHA256) {
				matches = true
				break
			}
		}
		if !matches {
			return false
		}
	}
	return true
}

// prepareAdoptedArtifacts imports the observed bytes of every accepted adopted
// package through one artifact store. Each import verifies the observed
// SHA-256 and every digest the candidate pinned. With a target it also records
// each package's descriptor view in a resolution cache; without one it only
// seeds the store for replay.
func prepareAdoptedArtifacts(
	ctx context.Context,
	root string,
	adopted []AdoptedRelease,
	target *resolve.Target,
) (*resolve.AnalysisCache, error) {
	if len(adopted) == 0 {
		return nil, nil
	}
	store, err := artifactstore.Open()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	var cache *resolve.AnalysisCache
	if target != nil {
		cache = resolve.NewAnalysisCache()
	}
	for _, release := range adopted {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if cache != nil && release.Analysis == nil {
			return nil, fmt.Errorf("missing local evidence for adopted package %s", release.Reference)
		}
		if name := release.Candidate.Filename; name == "" ||
			filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("invalid artifact filename %q", name)
		}
		observed := release.Path
		if !filepath.IsAbs(observed) {
			observed = filepath.Join(root, filepath.FromSlash(observed))
		}
		relative, ok := workspaceRelative(root, observed)
		if !ok || release.SHA256 == "" {
			return nil, fmt.Errorf("missing local evidence for adopted package %s", release.Reference)
		}
		if err := workspacewrite.SafePath(root, relative); err != nil {
			return nil, err
		}
		info, err := os.Lstat(observed)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("missing local evidence for adopted package %s", release.Reference)
		}
		candidate := release.Candidate
		candidate.Hashes = maps.Clone(release.Candidate.Hashes)
		if candidate.Hashes == nil {
			candidate.Hashes = make(map[string]string, 1)
		}
		if digest := candidate.Hashes["sha256"]; digest != "" &&
			!strings.EqualFold(digest, release.SHA256) {
			return nil, fmt.Errorf("sha256 checksum mismatch for adopted package %s", release.Reference)
		}
		candidate.Hashes["sha256"] = release.SHA256
		_, hashes, err := importFile(store.Store, observed, relative, candidate.Hashes)
		if err != nil {
			return nil, fmt.Errorf("store adopted package %s: %w", release.Reference, err)
		}
		if cache == nil {
			continue
		}
		view, err := release.Analysis.Descriptor(
			ctx,
			target.DescriptorLoader(release.Ecosystem),
			string(target.Minecraft),
		)
		if err != nil {
			return nil, fmt.Errorf("read descriptor for adopted package %s: %w", release.Reference, err)
		}
		cache.RecordVerified(release.Candidate, *target, release.Ecosystem, view.Modules, hashes)
	}
	return cache, nil
}

// localRuntimeSource offers the runtime files initialization observed as
// artifact sources. A file satisfies an artifact only when every trusted
// digest it pins, and the observed SHA-256, match its bytes; a name or role
// never stands in for that proof, so a launcher cannot satisfy its installer.
type localRuntimeSource struct {
	// mu guards the digests and store paths cached per file: acquisitions of
	// one server product run in parallel and share this source.
	mu    sync.Mutex
	root  string
	files []*localRuntimeFile
}

type localRuntimeFile struct {
	path, rel string
	digests   map[string]string
	// storePath is the verified artifact store object this file was imported
	// as. It carries no hash map, so every request builds its own.
	storePath string
}

func newLocalRuntimeSource(root string, files []AdoptedRuntimeFile) *localRuntimeSource {
	source := &localRuntimeSource{root: root}
	for _, observed := range files {
		digest := strings.ToLower(strings.TrimSpace(observed.ExpectedSHA256))
		path := observed.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, filepath.FromSlash(path))
		}
		if relative, ok := workspaceRelative(root, path); ok && digest != "" {
			source.files = append(source.files, &localRuntimeFile{
				path:    path,
				rel:     relative,
				digests: map[string]string{"sha256": digest},
			})
		}
	}
	return source
}

// acquire wraps next so an artifact the observed runtime proves is imported
// instead of transferred. Every other artifact keeps the normal path.
func (s *localRuntimeSource) acquire(
	ctx context.Context,
	next func(*catalog.Artifact) (artifact.MaterializedFile, error),
) func(*catalog.Artifact) (artifact.MaterializedFile, error) {
	return func(a *catalog.Artifact) (artifact.MaterializedFile, error) {
		if materialized, matched, err := s.materialize(ctx, a); err != nil || matched {
			return materialized, err
		}
		return next(a)
	}
}

// importServer imports the core and inputs a metadata resolver pinned and
// attaches a SHA-256 the observed runtime proved when the artifact lacks one.
// It never downloads or replaces an existing pin: the existing map is cloned
// and the proven digest added, so the caller's lock is never mutated and an
// artifact that already pins SHA-256 keeps its exact values. It reports
// whether any artifact gained a SHA-256.
func (s *localRuntimeSource) importServer(
	ctx context.Context,
	server document.LockedServer,
) (document.LockedServer, bool, error) {
	if len(s.files) == 0 {
		return server, false, nil
	}
	server.Inputs = slices.Clone(server.Inputs)
	changed := false
	artifacts := make([]*catalog.Artifact, 0, len(server.Inputs)+1)
	artifacts = append(artifacts, &server.Artifact)
	for i := range server.Inputs {
		artifacts = append(artifacts, &server.Inputs[i])
	}
	for _, artifact := range artifacts {
		materialized, matched, err := s.materialize(ctx, artifact)
		if err != nil {
			return server, false, err
		}
		if !matched {
			continue
		}
		proven := strings.ToLower(strings.TrimSpace(materialized.Hashes["sha256"]))
		if proven == "" || strings.TrimSpace(artifact.Hashes["sha256"]) != "" {
			continue
		}
		hashes := maps.Clone(artifact.Hashes)
		if hashes == nil {
			hashes = make(map[string]string, 1)
		}
		hashes["sha256"] = proven
		artifact.Hashes = hashes
		changed = true
	}
	return server, changed, nil
}

// withProvenServer returns old carrying the server digests the observed
// runtime proved. It keeps every pin the lock already carried and returns old
// itself when the proof added nothing, so an unchanged lock keeps its exact
// bytes and pointer identity.
func withProvenServer(
	old *document.Lock,
	server document.LockedServer,
	changed bool,
) *document.Lock {
	if !changed {
		return old
	}
	enriched := *old
	enriched.Server = server
	enriched.Packages = slices.Clone(old.Packages)
	enriched.Files = slices.Clone(old.Files)
	return &enriched
}

func (s *localRuntimeSource) materialize(
	ctx context.Context,
	a *catalog.Artifact,
) (artifact.MaterializedFile, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.files) == 0 || a.Embedded != "" {
		return artifact.MaterializedFile{}, false, nil
	}
	pins := make(map[string]string, len(a.Hashes)+1)
	for algorithm, digest := range a.Hashes {
		if digest = strings.ToLower(strings.TrimSpace(digest)); digest != "" {
			pins[algorithm] = digest
		}
	}
	file := s.match(pins)
	if file == nil {
		return artifact.MaterializedFile{}, false, nil
	}
	if file.storePath != "" {
		return file.materialization(pins), true, nil
	}
	if !s.confined(file) {
		return artifact.MaterializedFile{}, false, nil
	}
	pins["sha256"] = file.digests["sha256"]
	if err := ctx.Err(); err != nil {
		return artifact.MaterializedFile{}, false, err
	}
	if err := s.importFile(file, pins); err != nil {
		return artifact.MaterializedFile{}, false, err
	}
	return file.materialization(pins), true, nil
}

// materialization returns a fresh digest map for one request: the store's
// SHA-256 and only the digests that request pinned. The cached digest map
// stays private, so a later request cannot change a map already returned to
// an earlier caller.
func (f *localRuntimeFile) materialization(requested map[string]string) artifact.MaterializedFile {
	hashes := make(map[string]string, len(requested)+1)
	hashes["sha256"] = f.digests["sha256"]
	for algorithm := range requested {
		hashes[algorithm] = f.digests[algorithm]
	}
	return artifact.MaterializedFile{Path: f.storePath, Hashes: hashes}
}

func (s *localRuntimeSource) match(pins map[string]string) *localRuntimeFile {
	if len(pins) == 0 {
		return nil
	}
	for _, file := range s.files {
		if file.matches(s, pins) {
			return file
		}
	}
	return nil
}

// matches first rejects files any known digest already disproves, then
// digests only the missing pinned algorithms and the observed SHA-256 in one
// pass over the file. Computed digests are cached before comparison, so a
// non-matching file never hashes twice.
func (f *localRuntimeFile) matches(s *localRuntimeSource, pins map[string]string) bool {
	needed := map[string]string{"sha256": f.digests["sha256"]}
	for algorithm, digest := range pins {
		if known := f.digests[algorithm]; known != "" {
			if !strings.EqualFold(known, digest) {
				return false
			}
		} else {
			needed[algorithm] = ""
		}
	}
	if len(needed) == 1 {
		return true
	}
	if !s.confined(f) {
		return false
	}
	computed, err := DigestFile(f.path, needed)
	if err != nil {
		return false
	}
	maps.Copy(f.digests, computed)
	for algorithm, digest := range pins {
		if !strings.EqualFold(f.digests[algorithm], digest) {
			return false
		}
	}
	return true
}

func (s *localRuntimeSource) confined(file *localRuntimeFile) bool {
	info, err := os.Lstat(file.path)
	return err == nil && info.Mode().IsRegular() &&
		workspacewrite.SafePath(s.root, file.rel) == nil
}

func (s *localRuntimeSource) importFile(
	file *localRuntimeFile,
	expected map[string]string,
) error {
	store, err := artifactstore.Open()
	if err != nil {
		return err
	}
	defer store.Close()
	object, _, err := importFile(store.Store, file.path, file.rel, expected)
	if err != nil {
		return err
	}
	file.storePath = store.Path(object.Digest)
	return nil
}
