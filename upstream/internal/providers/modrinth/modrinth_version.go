package modrinth

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mclucy/lucy/catalog"
)

var (
	ErrNoVersion = errors.New("modrinth version not found")
	ErrNoProject = errors.New("modrinth project not found")
)

// listVersions has a known crash risk: this path has caused segmentation faults.
func (p *Provider) listVersions(
	ctx context.Context,
	slug catalog.Name,
) (versions []*versionResponse, err error) {
	var out []*versionResponse
	if err := p.requestJSON(
		ctx,
		versionsUrl(slug),
		&out,
		ErrNoProject,
	); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Provider) getVersionById(
	ctx context.Context,
	id string,
) (v *versionResponse, err error) {
	v = &versionResponse{}
	if err := p.requestJSON(ctx, versionUrl(id), v, ErrNoVersion); err != nil {
		return nil, err
	}
	return
}

// getVersionsBatch reads one bounded batch of version IDs. IDs the API omits
// stay absent from the map.
func (p *Provider) getVersionsBatch(
	ctx context.Context,
	ids []string,
) (map[string]*versionResponse, error) {
	var list []*versionResponse
	if err := p.requestJSON(ctx, versionsByIdsUrl(ids), &list, nil); err != nil {
		return nil, err
	}
	out := make(map[string]*versionResponse, len(list))
	for _, v := range list {
		if v != nil {
			out[v.ID] = v
		}
	}
	return out, nil
}

// getVersionsByIds resolves version IDs in deterministic bounded batches. A
// failed batch records its error against every ID it carried; an ID the API
// omits has neither an entry nor an error.
func (p *Provider) getVersionsByIds(
	ctx context.Context,
	ids []string,
) (map[string]*versionResponse, map[string]error) {
	versions := make(map[string]*versionResponse, len(ids))
	errs := make(map[string]error)
	for batch := range slices.Chunk(batchIds(ids), batchSize) {
		found, err := p.getVersionsBatch(ctx, batch)
		if err != nil {
			for _, id := range batch {
				errs[id] = err
			}
			continue
		}
		for id, v := range found {
			versions[id] = v
		}
	}
	return versions, errs
}

// versionByIdLookup answers one pinned dependency version. A nil lookup falls
// back to the single-version endpoint.
type versionByIdLookup func(context.Context, string) (*versionResponse, error)

// dependencyVersionIds returns the pinned dependency version IDs that a
// version does not also label with a project ID.
func dependencyVersionIds(versions ...*versionResponse) []string {
	var ids []string
	for _, v := range versions {
		if v == nil {
			continue
		}
		for _, d := range v.Dependencies {
			if d.VersionId != "" && d.ProjectId == "" {
				ids = append(ids, d.VersionId)
			}
		}
	}
	return ids
}

// requireVersionProject returns the version only when it names its project.
// A pinned dependency needs that project to become a dependency edge, so a
// record without one is an invalid response rather than a usable version.
func requireVersionProject(
	id string,
	version *versionResponse,
) (*versionResponse, error) {
	if version.ProjectId == "" {
		return nil, fmt.Errorf(
			"%w: version %s has no project",
			ErrInvalidAPIResponse,
			id,
		)
	}
	return version, nil
}

// pinnedVersionLookup resolves every pinned dependency version across versions
// in one bounded fan-out. A nil result means no version pins a dependency, so
// the caller makes no request. A version the batch left without a project is
// malformed, so the caller reads that ID from the single-version endpoint
// instead; the retry is remembered so duplicate dependency edges share one
// request. A failed batch also retries that way when a caller asks for it. A
// valid bulk result stays authoritative, an ID the API omitted is not found,
// and a failed retry keeps both causes. A retry that resolves no project is an
// invalid response, never a usable dependency.
func (p *Provider) pinnedVersionLookup(
	ctx context.Context,
	versions []*versionResponse,
) versionByIdLookup {
	ids := dependencyVersionIds(versions...)
	if len(ids) == 0 {
		return nil
	}
	type attempt struct {
		version *versionResponse
		err     error
	}
	found, errs := p.getVersionsByIds(ctx, ids)
	attempts := make(map[string]attempt)
	return func(callCtx context.Context, id string) (*versionResponse, error) {
		if err := callCtx.Err(); err != nil {
			return nil, err
		}
		var batchErr error
		if v := found[id]; v != nil {
			if v.ProjectId != "" {
				return v, nil
			}
			batchErr = fmt.Errorf(
				"%w: version %s has no project",
				ErrInvalidAPIResponse,
				id,
			)
		} else {
			batchErr = errs[id]
		}
		if batchErr == nil {
			return nil, fmt.Errorf("%w: %s", ErrNoVersion, id)
		}
		if prior, ok := attempts[id]; ok {
			return prior.version, prior.err
		}
		v, err := p.getVersionById(callCtx, id)
		if err == nil {
			v, err = requireVersionProject(id, v)
		}
		if err != nil {
			v = nil
			err = errors.Join(batchErr, err)
		}
		attempts[id] = attempt{version: v, err: err}
		return v, err
	}
}

// resolveVersionById prefers the caller's batch lookup, so a candidate never
// fans out per dependency. Without one it reads the single-version endpoint,
// preserving the original identification path, and rejects a project-less
// record like the batch lookup does.
func (p *Provider) resolveVersionById(
	ctx context.Context,
	lookup versionByIdLookup,
	id string,
) (*versionResponse, error) {
	if lookup != nil {
		return lookup(ctx, id)
	}
	version, err := p.getVersionById(ctx, id)
	if err != nil {
		return nil, err
	}
	return requireVersionProject(id, version)
}

// versionSupportsLoader reports whether a server in loader can load the
// version. The requested server ecosystem is the capability, so a superset
// accepts its base loader: Quilt accepts Fabric builds and Paper accepts
// Bukkit plugins, never the reverse. An unspecified request accepts any
// loader tag.
func versionSupportsLoader(
	version *versionResponse,
	loader catalog.Ecosystem,
) bool {
	if loader == catalog.EcosystemUnspecified {
		return true
	}
	for _, l := range version.Loaders {
		if loader.Satisfy(catalog.Ecosystem(l)) {
			return true
		}
	}
	return false
}
