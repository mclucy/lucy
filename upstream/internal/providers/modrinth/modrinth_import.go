package modrinth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

var (
	ErrNotModpack = errors.New("modrinth project is not a modpack")
	ErrNoMrpack   = errors.New("modrinth version has no downloadable .mrpack file")
)

const (
	modpackProjectType = "modpack"
	mrpackExtension    = ".mrpack"
)

// ResolveImportPack resolves a Modrinth modpack and an optional version
// selector to its downloadable .mrpack file. project accepts a slug or a
// project ID. version accepts a version number or a version ID; an empty
// version selects the newest release, falling back to the newest version of
// any type when the project has no release.
func (p *Provider) ResolveImportPack(
	ctx context.Context,
	project, version string,
) (upstream.ImportPack, error) {
	if err := ctx.Err(); err != nil {
		return upstream.ImportPack{}, err
	}
	proj, err := p.getProjectById(ctx, project)
	if err != nil {
		return upstream.ImportPack{}, err
	}
	if proj.ProjectType != modpackProjectType {
		return upstream.ImportPack{}, fmt.Errorf(
			"%w: %s is a %s",
			ErrNotModpack,
			proj.Slug,
			proj.ProjectType,
		)
	}
	// The version endpoint accepts the project ID, which is stable across
	// slug renames.
	versions, err := p.listVersions(ctx, catalog.Name(proj.ID))
	if err != nil {
		return upstream.ImportPack{}, err
	}
	selected, err := selectImportVersion(versions, version)
	if err != nil {
		return upstream.ImportPack{}, err
	}
	file, ok := mrpackFile(selected.Files)
	if !ok {
		return upstream.ImportPack{}, fmt.Errorf(
			"%w: %s %s",
			ErrNoMrpack,
			proj.Slug,
			selected.VersionNumber,
		)
	}
	return upstream.ImportPack{
		Provider: catalog.ProviderModrinth,
		Project:  proj.Slug,
		Name:     proj.Title,
		Version:  selected.VersionNumber,
		Filename: file.Filename,
		URL:      file.URL,
		Hashes:   fileHashes(file),
	}, nil
}

// selectImportVersion picks the version an import resolves to. A non-empty
// selector matches a version number or version ID exactly. An empty selector
// picks the newest release, falling back to the newest version of any type.
func selectImportVersion(
	versions []*versionResponse,
	selector string,
) (*versionResponse, error) {
	if selector != "" {
		for _, v := range versions {
			if v != nil && (v.VersionNumber == selector || v.ID == selector) {
				return v, nil
			}
		}
		return nil, fmt.Errorf("%w: no version matches %q", ErrNoVersion, selector)
	}
	if v := newestVersion(versions, isReleaseVersion); v != nil {
		return v, nil
	}
	if v := newestVersion(versions, nil); v != nil {
		return v, nil
	}
	return nil, fmt.Errorf("%w: project has no versions", ErrNoVersion)
}

func isReleaseVersion(v *versionResponse) bool {
	return v.VersionType == "release"
}

// newestVersion returns the version with the latest DatePublished that keep
// accepts, or nil when none is accepted. A nil keep accepts every version.
func newestVersion(
	versions []*versionResponse,
	keep func(*versionResponse) bool,
) *versionResponse {
	var newest *versionResponse
	for _, v := range versions {
		if v == nil || (keep != nil && !keep(v)) {
			continue
		}
		if newest == nil || v.DatePublished.After(newest.DatePublished) {
			newest = v
		}
	}
	return newest
}

// mrpackFile chooses the .mrpack file of a version: the primary one when it
// is marked, otherwise the first downloadable .mrpack. The bool is false when
// the version has no such file.
func mrpackFile(files []fileResponse) (fileResponse, bool) {
	var found fileResponse
	ok := false
	for _, f := range files {
		if !isDownloadableMrpack(f) {
			continue
		}
		if f.IsPrimary {
			return f, true
		}
		if !ok {
			found, ok = f, true
		}
	}
	return found, ok
}

func isDownloadableMrpack(f fileResponse) bool {
	return f.URL != "" &&
		strings.HasSuffix(strings.ToLower(f.Filename), mrpackExtension)
}

// fileHashes returns the file's digests keyed by algorithm name.
func fileHashes(f fileResponse) map[string]string {
	hashes := map[string]string{}
	if f.Hashes.Sha512 != "" {
		hashes["sha512"] = f.Hashes.Sha512
	}
	if f.Hashes.Sha1 != "" {
		hashes["sha1"] = f.Hashes.Sha1
	}
	return hashes
}
