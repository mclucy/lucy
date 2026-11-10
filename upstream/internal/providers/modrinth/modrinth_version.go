package modrinth

import (
	"context"
	"errors"

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
