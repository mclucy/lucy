package modrinth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/mclucy/lucy/slugmap"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
)

func getProjectId(slug types.ProjectName) (id string, err error) {
	res, err := http.Get(projectUrl(string(slug)))
	if err != nil {
		return "", fmt.Errorf("modrinth: request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", ENoProject
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("modrinth: failed to read response: %w", err)
	}
	modrinthProject := projectResponse{}
	err = json.Unmarshal(data, &modrinthProject)
	if err != nil {
		return "", ENoProject
	}
	id = modrinthProject.Id
	return
}

func getProjectById(id string) (project *projectResponse, err error) {
	res, err := http.Get(projectUrl(id))
	if err != nil {
		return nil, fmt.Errorf("modrinth: request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, ENoProject
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("modrinth: failed to read response: %w", err)
	}
	project = &projectResponse{}
	err = json.Unmarshal(data, project)
	if err != nil {
		return nil, ENoProject
	}
	return
}

func getProjectByName(slug types.ProjectName) (
	project *projectResponse,
	err error,
) {
	if canonical, ok := slugmap.Default().GetLoose(types.SourceModrinth, string(slug)); ok {
		slug = types.ProjectName(canonical)
	}
	res, err := http.Get(projectUrl(string(slug)))
	if err != nil {
		return nil, fmt.Errorf("modrinth: request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, ENoProject
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("modrinth: failed to read response: %w", err)
	}
	project = &projectResponse{}
	err = json.Unmarshal(data, project)
	if err != nil {
		return nil, ENoProject
	}
	return
}

func getProjectMembers(id string) (
	members []*memberResponse,
	err error,
) {
	res, err := http.Get(projectMemberUrl(id))
	if err != nil {
		return nil, fmt.Errorf("modrinth: request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, ENoMember
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("modrinth: failed to read response: %w", err)
	}
	err = json.Unmarshal(data, &members)
	if err != nil {
		return nil, ENoMember
	}
	return members, nil
}

var ErrorInvalidDependency = errors.New("invalid dependency")

func DependencyToPackage(
	dependent types.PackageId,
	dependency *dependenciesResponse,
) (
	p types.PackageId,
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
		version, err = latestVersion(syntax.ToProjectName(project.Slug))
		if err != nil {
			return p, fmt.Errorf("resolve dependency latest version: %w", err)
		}
		p.Version = types.VersionLatest
	} else {
		return p, ErrorInvalidDependency
	}

	p.Name = syntax.ToProjectName(project.Slug)
	p.Version = types.RawVersion(version.VersionNumber)

	return p, nil
}
