package install

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

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

// prepareAdoptedArtifacts pins the already observed bytes in the store and
// supplies their descriptor view to the resolver without a provider download.
func prepareAdoptedArtifacts(ctx context.Context, root string, adopted []AdoptedRelease, target resolve.Target) (*resolve.AnalysisCache, error) {
	store, err := artifactstore.Open()
	if err != nil {
		return nil, err
	}
	defer store.Close()
	cache := resolve.NewAnalysisCache()
	for _, release := range adopted {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := release.Candidate.Filename
		if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
			return nil, fmt.Errorf("invalid artifact filename %q", name)
		}
		relative, ok := workspaceRelative(root, release.Path)
		if !ok || release.SHA256 == "" || release.Analysis == nil {
			return nil, fmt.Errorf("missing local evidence for adopted package %s", release.Reference)
		}
		if err := workspacewrite.SafePath(root, relative); err != nil {
			return nil, err
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("adopted package %s is not a regular file", relative)
		}
		expected := maps.Clone(release.Candidate.Hashes)
		if expected == nil {
			expected = make(map[string]string, 1)
		}
		if digest := expected["sha256"]; digest != "" && !strings.EqualFold(digest, release.SHA256) {
			return nil, fmt.Errorf("sha256 checksum mismatch for adopted package %s", relative)
		}
		expected["sha256"] = release.SHA256
		_, hashes, err := importFile(store.Store, path, relative, expected)
		if err != nil {
			return nil, fmt.Errorf("store adopted package %s: %w", relative, err)
		}
		view, err := release.Analysis.Descriptor(ctx, target.DescriptorLoader(release.Ecosystem), string(target.Minecraft))
		if err != nil {
			return nil, fmt.Errorf("read descriptor for adopted package %s: %w", relative, err)
		}
		cache.RecordVerified(release.Candidate, target, release.Ecosystem, view.Modules, hashes)
	}
	return cache, nil
}
