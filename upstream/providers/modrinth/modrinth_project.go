package modrinth

import (
	"context"
	"errors"
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	versionpkg "github.com/mclucy/lucy/version"
)

func (p *Provider) getProjectId(
	ctx context.Context,
	slug catalog.Name,
) (id string, err error) {
	modrinthProject := projectResponse{}
	if err := p.requestJSON(
		ctx,
		projectUrl(string(slug)),
		&modrinthProject,
		ErrNoProject,
	); err != nil {
		return "", err
	}
	id = modrinthProject.ID
	return
}

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

func (p *Provider) getProjectMembers(
	ctx context.Context,
	id string,
) (members []*memberResponse, err error) {
	if err := p.requestJSON(
		ctx,
		projectMemberUrl(id),
		&members,
		ErrNoMember,
	); err != nil {
		return nil, err
	}
	return members, nil
}

var ErrInvalidDependency = errors.New("modrinth: invalid dependency")

func (p *Provider) DependencyToPackage(
	ctx context.Context,
	dependent catalog.VersionedRef,
	dependency *dependenciesResponse,
) (
	pkg catalog.VersionedRef,
	err error,
) {
	var version *versionResponse
	var project *projectResponse

	// Modrinth dependencies do not cross platforms: a dependency's platform
	// is taken from the dependent package, not from the dependency itself.
	pkg.Ecosystem = dependent.Ecosystem

	if dependency.VersionId != "" && dependency.ProjectId != "" {
		version, err = p.getVersionById(ctx, dependency.VersionId)
		if err != nil {
			return pkg, fmt.Errorf("resolve dependency version: %w", err)
		}
		project, err = p.getProjectById(ctx, dependency.ProjectId)
		if err != nil {
			return pkg, fmt.Errorf("resolve dependency project: %w", err)
		}
	} else if dependency.VersionId != "" {
		version, err = p.getVersionById(ctx, dependency.VersionId)
		if err != nil {
			return pkg, fmt.Errorf("resolve dependency version: %w", err)
		}
		project, err = p.getProjectById(ctx, version.ProjectId)
		if err != nil {
			return pkg, fmt.Errorf("resolve dependency project: %w", err)
		}
	} else if dependency.ProjectId != "" {
		project, err = p.getProjectById(ctx, dependency.ProjectId)
		if err != nil {
			return pkg, fmt.Errorf("resolve dependency project: %w", err)
		}
		_, err = p.latestVersion(
			ctx,
			input.ToProjectName(project.Slug),
			dependent.Ecosystem,
			versionpkg.Unknown,
			versionpkg.Any,
		)
		if err != nil {
			return pkg, fmt.Errorf("resolve dependency latest version: %w", err)
		}
		pkg.Name = input.ToProjectName(project.Slug)
		pkg.Version = versionpkg.Any
		return pkg, nil
	} else {
		return pkg, ErrInvalidDependency
	}

	pkg.Name = input.ToProjectName(project.Slug)
	pkg.Version = versionpkg.Version(version.VersionNumber)

	return pkg, nil
}
