package modrinth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/mclucy/lucy/syntax"

	"github.com/mclucy/lucy/types"
)

func getProjectId(slug types.ProjectName) (id string, err error) {
	res, _ := http.Get(projectUrl(string(slug)))
	modrinthProject := projectResponse{}
	data, _ := io.ReadAll(res.Body)
	err = json.Unmarshal(data, &modrinthProject)
	if err != nil {
		return "", ENoProject
	}
	id = modrinthProject.Id
	return
}

func getProjectById(id string) (project *projectResponse, err error) {
	res, _ := http.Get(projectUrl(id))
	data, _ := io.ReadAll(res.Body)
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
	res, _ := http.Get(projectUrl(string(slug)))
	data, _ := io.ReadAll(res.Body)
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
	res, _ := http.Get(projectMemberUrl(id))
	data, _ := io.ReadAll(res.Body)
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
		version, _ = getVersionById(dependency.VersionId)
		project, _ = getProjectById(dependency.ProjectId)
	} else if dependency.VersionId != "" {
		version, _ = getVersionById(dependency.VersionId)
		project, _ = getProjectById(version.ProjectId)
	} else if dependency.ProjectId != "" {
		project, _ = getProjectById(dependency.ProjectId)
		// This is not safe, TODO: use better inference method
		version, _ = latestVersion(syntax.ToProjectName(project.Slug))
		p.Version = types.VersionLatest
	} else {
		return p, ErrorInvalidDependency
	}

	p.Name = syntax.ToProjectName(project.Slug)
	p.Version = types.RawVersion(version.VersionNumber)

	return p, nil
}
