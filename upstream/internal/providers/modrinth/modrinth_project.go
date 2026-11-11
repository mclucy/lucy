package modrinth

import (
	"context"
	"errors"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
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

// ServerSideByName reports the project's dedicated-server label. A project the
// provider does not host is not an error.
func (p *Provider) ServerSideByName(
	ctx context.Context,
	name string,
) (upstream.ProjectServerSide, bool, error) {
	project, err := p.getProjectByName(ctx, catalog.Name(name))
	if errors.Is(err, ErrNoProject) {
		return upstream.ProjectServerSide{}, false, nil
	}
	if err != nil {
		return upstream.ProjectServerSide{}, false, err
	}
	return upstream.ProjectServerSide{
		ServerSide: upstream.ServerSide(project.ServerSide),
		ClientSide: project.ClientSide,
		Title:      project.Title,
	}, true, nil
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
