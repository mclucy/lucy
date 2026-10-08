package modrinth

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
)

func getProjectId(slug types.BarePackageName) (id string, err error) {
	modrinthProject := projectResponse{}
	if err := requestJSON(
		projectUrl(string(slug)),
		&modrinthProject,
		ErrNoProject,
	); err != nil {
		return "", err
	}
	id = modrinthProject.Id
	return
}

func getProjectById(id string) (project *projectResponse, err error) {
	project = &projectResponse{}
	if err := requestJSON(projectUrl(id), project, ErrNoProject); err != nil {
		return nil, err
	}
	return
}

func getProjectByName(slug types.BarePackageName) (
	project *projectResponse,
	err error,
) {
	project = &projectResponse{}
	if err := requestJSON(
		projectUrl(string(slug)),
		project,
		ErrNoProject,
	); err != nil {
		return nil, err
	}
	return project, nil
}

func getProjectMembers(id string) (
	members []*memberResponse,
	err error,
) {
	if err := requestJSON(
		projectMemberUrl(id),
		&members,
		ErrNoMember,
	); err != nil {
		return nil, err
	}
	return members, nil
}

var ErrInvalidDependency = errors.New("modrinth: invalid dependency")

func DependencyToPackage(
	dependent types.VersionedPackageRef,
	dependency *dependenciesResponse,
) (
	p types.VersionedPackageRef,
	err error,
) {
	var version *versionResponse
	var project *projectResponse

	// Modrinth dependencies do not cross platforms: a dependency's platform
	// is taken from the dependent package, not from the dependency itself.
	p.Eco = dependent.Eco

	if dependency.VersionId != "" && dependency.ProjectId != "" {
		version, err = getVersionById(dependency.VersionId)
		if err != nil {
			return p, fmt.Errorf("resolve dependency version: %w", err)
		}
		project, err = getProjectById(dependency.ProjectId)
		if err != nil {
			return p, fmt.Errorf("resolve dependency project: %w", err)
		}
	} else if dependency.VersionId != "" {
		version, err = getVersionById(dependency.VersionId)
		if err != nil {
			return p, fmt.Errorf("resolve dependency version: %w", err)
		}
		project, err = getProjectById(version.ProjectId)
		if err != nil {
			return p, fmt.Errorf("resolve dependency project: %w", err)
		}
	} else if dependency.ProjectId != "" {
		project, err = getProjectById(dependency.ProjectId)
		if err != nil {
			return p, fmt.Errorf("resolve dependency project: %w", err)
		}
		version, err = latestCompatibleVersion(
			input.ToProjectName(project.Slug),
			dependent.Eco,
			types.VersionUnknown,
		)
		if err != nil {
			return p, fmt.Errorf("resolve dependency latest version: %w", err)
		}
		p.Name = input.ToProjectName(project.Slug)
		p.Version = types.VersionAny
		return p, nil
	} else {
		return p, ErrInvalidDependency
	}

	p.Name = input.ToProjectName(project.Slug)
	p.Version = types.BareVersion(version.VersionNumber)

	return p, nil
}
