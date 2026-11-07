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

func versionSupportsLoader(
	version *versionResponse,
	loader catalog.Ecosystem,
) bool {
	for _, l := range version.Loaders {
		if catalog.Ecosystem(l).Satisfy(loader) {
			return true
		}
	}
	return false
}
