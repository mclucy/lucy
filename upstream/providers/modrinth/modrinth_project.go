package modrinth

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/knownpkgs"
	"github.com/mclucy/lucy/types"
)

func getProjectId(slug types.BarePackageName) (id string, err error) {
	modrinthProject := projectResponse{}
	if err := requestJSON(projectUrl(string(slug)), &modrinthProject, ENoProject); err != nil {
		return "", ENoProject
	}
	id = modrinthProject.Id
	return
}

func getProjectById(id string) (project *projectResponse, err error) {
	project = &projectResponse{}
	if err := requestJSON(projectUrl(id), project, ENoProject); err != nil {
		return nil, ENoProject
	}
	return
}

func getProjectByName(slug types.BarePackageName) (
	project *projectResponse,
	err error,
) {
	tryFetch := func(target types.BarePackageName) (
		*projectResponse,
		error,
	) {
		project := &projectResponse{}
		if err := requestJSON(projectUrl(string(target)), project, ENoProject); err != nil {
			return nil, ENoProject
		}
		return project, nil
	}

	project, err = tryFetch(slug)
	if err == nil {
		return project, nil
	}

	if canonical, ok := knownpkgs.Default().Session().Lookup(
		types.SourceModrinth,
		string(slug),
	); ok && canonical != string(slug) {
		return tryFetch(types.BarePackageName(canonical))
	}

	return nil, err
}

func getProjectMembers(id string) (
	members []*memberResponse,
	err error,
) {
	if err := requestJSON(projectMemberUrl(id), &members, ENoMember); err != nil {
		return nil, ENoMember
	}
	return members, nil
}

var ErrorInvalidDependency = errors.New("invalid dependency")

func DependencyToPackage(
	dependent types.VersionedPackageRef,
	dependency *dependenciesResponse,
) (
	p types.VersionedPackageRef,
	err error,
) {
	var version *versionResponse
	var project *projectResponse

	// I don't see a case where a package would depend on a project on another
	// platform. So, we can safely assume that the platform of the dependent
	// package is the same as the platform of the dependency.
	p.Platform = dependent.Platform

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
		// This is not safe, TODO: use better inference method
		version, err = latestCompatibleVersion(
			input.ToProjectName(project.Slug),
			dependent.Platform,
		)
		if err != nil {
			return p, fmt.Errorf("resolve dependency latest version: %w", err)
		}
		p.Name = input.ToProjectName(project.Slug)
		p.Version = types.VersionCompatible
		return p, nil
	} else {
		return p, ErrorInvalidDependency
	}

	p.Name = input.ToProjectName(project.Slug)
	p.Version = types.BareVersion(version.VersionNumber)

	return p, nil
}
