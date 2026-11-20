package install

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/artifact"
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
// package. With a target it also records each package's descriptor view in a
// resolution cache; without one it only seeds the store for replay.
func prepareAdoptedArtifacts(
	ctx context.Context,
	root string,
	adopted []AdoptedRelease,
	target *resolve.Target,
) (*resolve.AnalysisCache, error) {
	if len(adopted) == 0 {
		return nil, nil
	}
	var cache *resolve.AnalysisCache
	if target != nil {
		cache = resolve.NewAnalysisCache()
	}
	for _, release := range adopted {
		if cache != nil && release.Analysis == nil {
			return nil, fmt.Errorf("missing local evidence for adopted package %s", release.Reference)
		}
		if name := release.Candidate.Filename; name == "" ||
			filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("invalid artifact filename %q", name)
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
		source := newLocalRuntimeSource(root, []AdoptedRuntimeFile{{
			Path:           release.Path,
			ExpectedSHA256: release.SHA256,
		}})
		materialized, matched, err := source.materialize(ctx, &candidate.Artifact)
		if err != nil {
			return nil, fmt.Errorf("store adopted package %s: %w", release.Reference, err)
		}
		if !matched {
			return nil, fmt.Errorf("missing local evidence for adopted package %s", release.Reference)
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
		cache.RecordVerified(release.Candidate, *target, release.Ecosystem, view.Modules, materialized.Hashes)
	}
	return cache, nil
}

// localRuntimeSource offers the runtime files initialization observed as
// artifact sources. A file satisfies an artifact only when every trusted
// digest it pins, and the observed SHA-256, match its bytes; a name or role
// never stands in for that proof, so a launcher cannot satisfy its installer.
type localRuntimeSource struct {
	root  string
	files []*localRuntimeFile
}

type localRuntimeFile struct {
	path, rel string
	digests   map[string]string
	object    *artifact.MaterializedFile
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
// attaches the digests the import proved. It never downloads; an artifact the
// observed runtime cannot prove keeps its pins.
func (s *localRuntimeSource) importServer(
	ctx context.Context,
	server document.LockedServer,
) (document.LockedServer, error) {
	if len(s.files) == 0 {
		return server, nil
	}
	server.Inputs = slices.Clone(server.Inputs)
	artifacts := make([]*catalog.Artifact, 0, len(server.Inputs)+1)
	artifacts = append(artifacts, &server.Artifact)
	for i := range server.Inputs {
		artifacts = append(artifacts, &server.Inputs[i].Artifact)
	}
	for _, artifact := range artifacts {
		materialized, matched, err := s.materialize(ctx, artifact)
		if err != nil {
			return server, err
		}
		if matched {
			artifact.Hashes = materialized.Hashes
		}
	}
	return server, nil
}

func (s *localRuntimeSource) materialize(
	ctx context.Context,
	a *catalog.Artifact,
) (artifact.MaterializedFile, bool, error) {
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
	if file.object != nil {
		for algorithm := range pins {
			file.object.Hashes[algorithm] = file.digests[algorithm]
		}
		return *file.object, true, nil
	}
	if !s.confined(file) {
		return artifact.MaterializedFile{}, false, nil
	}
	pins["sha256"] = file.digests["sha256"]
	if err := ctx.Err(); err != nil {
		return artifact.MaterializedFile{}, false, err
	}
	materialized, err := s.importFile(file, pins)
	if err != nil {
		return artifact.MaterializedFile{}, false, err
	}
	file.object = &materialized
	return materialized, true, nil
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
) (artifact.MaterializedFile, error) {
	input := catalog.Artifact{Filename: file.rel, Hashes: expected}
	store := &downloadStore{injected: func(*catalog.Artifact) (artifact.MaterializedFile, error) {
		return artifact.MaterializedFile{Path: file.path}, nil
	}}
	return store.injectedMaterialize(&input)
}
