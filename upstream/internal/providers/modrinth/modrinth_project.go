package modrinth

import (
	"context"
	"errors"
	"slices"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/internal/providers/retry"
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

// getProjectsByIds resolves project IDs in deterministic bounded batches. A
// failed batch records its error against every ID it carried; an ID the API
// omits has neither an entry nor an error. A throttle ends the fan-out; the
// batches not yet sent record the same throttle instead of asking the provider
// again before its delay passes.
func (p *Provider) getProjectsByIds(
	ctx context.Context,
	ids []string,
) (map[string]*projectResponse, map[string]error) {
	projects := make(map[string]*projectResponse, len(ids))
	errs := make(map[string]error)
	var throttle error
	for batch := range slices.Chunk(batchIds(ids), batchSize) {
		if throttle != nil {
			for _, id := range batch {
				errs[id] = throttle
			}
			continue
		}
		var list []*projectResponse
		if err := p.requestJSON(ctx, projectsByIdsUrl(batch), &list, nil); err != nil {
			for _, id := range batch {
				errs[id] = err
			}
			if retry.Throttled(err) {
				throttle = err
			}
			continue
		}
		for _, project := range list {
			if project != nil {
				projects[project.ID] = project
			}
		}
	}
	return projects, errs
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
