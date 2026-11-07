package modrinth

import (
	"context"

	"github.com/mclucy/lucy/catalog"
)

func (p *Provider) getProjectById(
	ctx context.Context,
	id string,
) (project *projectResponse, err error) {
	project = &projectResponse{}
	if err := p.requestJSON(
		ctx,
		projectUrl(id),
		project,
		ErrNoProject,
	); err != nil {
		return nil, err
	}
	return
}

func (p *Provider) getProjectByName(
	ctx context.Context,
	slug catalog.Name,
) (project *projectResponse, err error) {
	project = &projectResponse{}
	if err := p.requestJSON(
		ctx,
		projectUrl(string(slug)),
		project,
		ErrNoProject,
	); err != nil {
		return nil, err
	}
	return project, nil
}
