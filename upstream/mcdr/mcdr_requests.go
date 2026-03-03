package mcdr

import (
	"encoding/json"
	"fmt"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/github"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"

	"github.com/sahilm/fuzzy"
)

const (
	pluginCatalogueRepoEndpoint = `https://api.github.com/repos/MCDReforged/PluginCatalogue/contents/`
	branchMaster                = "?ref=master"
	branchCatalogue             = "?ref=catalogue" // I haven't figured out the difference yet
	branchMeta                  = "?ref=meta"
)

func search(query string) (mcdrSearchResult, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + ("plugins/") + branchCatalogue
	err, msg, items := github.GetDirectoryFromGitHub(ghEndpoint)
	if err != nil {
		return nil, err
	}
	if msg != nil && msg.Message != "" {
		return nil, fmt.Errorf("%w: %s", ErrorGhApi, msg.Message)
	}

	pluginIds := make([]string, 0)
	for _, file := range items {
		pluginIds = append(pluginIds, file.Name)
	}

	matches := fuzzy.Find(query, pluginIds)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = append(result, pluginIds[match.Index])
	}
	return result, nil
}

func getInfo(id string) (*pluginInfo, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + ("plugins/") + id + "/plugin_info.json" + branchMaster
	var data []byte
	err, msg, data := github.GetFileFromGitHub(ghEndpoint)
	if err != nil {
		return nil, err
	}
	if msg != nil && msg.Message != "" {
		if msg.Status == "404" {
			return nil, ErrPluginNotFound(id)
		}
		return nil, fmt.Errorf("%w: %s", ErrorGhApi, msg.Message)
	}

	var res github.GhItem
	err = json.Unmarshal(data, &res)
	if err != nil {
		return nil, err
	}

	var info pluginInfo
	err = json.Unmarshal(data, &info)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func getMeta(id string) (*pluginMeta, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + id + "/meta.json" + branchMeta
	err, msg, data := github.GetFileFromGitHub(ghEndpoint)
	if err != nil {
		return nil, err
	}
	if msg != nil && msg.Message != "" {
		if msg.Status == "404" {
			return nil, ErrPluginNotFound(id)
		}
		return nil, fmt.Errorf("%w: %s", ErrorGhApi, msg.Message)
	}

	var meta pluginMeta
	err = json.Unmarshal(data, &meta)
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

func getRelease(id string, version types.RawVersion) (*release, error) {
	history, err := getReleaseHistory(id)
	if err != nil {
		return nil, err
	}

	if version == types.VersionLatest {
		return &history.Releases[history.LatestVersionIndex], nil
	}

	for _, rel := range history.Releases {
		if rel.Meta.Version == version.String() {
			return &rel, nil
		}
	}
	return nil, ErrVersionNotFound(id, version.String())
}

func getLatestRelease(id string) (*release, error) {
	history, err := getReleaseHistory(id)
	if err != nil {
		return nil, err
	}
	return &history.Releases[history.LatestVersionIndex], nil
}

func getLatestCompatibleRelease(id string) (*release, error) {
	serverInfo := probe.ServerInfo()
	history, err := getReleaseHistory(id)
	if err != nil {
		return nil, err
	}

	localMcdrVersion := serverInfo.Environments.Mcdr.Version
	mcdrPackage := types.PackageId{
		Platform: types.Mcdr,
		Name:     "mcdreforged",
		Version:  localMcdrVersion,
	}
	for _, rel := range history.Releases {
		for k, v := range rel.Meta.Dependencies {
			if k == "mcdreforged" {
				dep := types.Dependency{
					Id: mcdrPackage,
					Constraint: dependency.ParseRange(
						v,
						dependency.DialectNpmSemver,
						types.Semver,
					),
					Mandatory: true,
				}
				if dep.Satisfy(
					mcdrPackage,
					dependency.Parse(localMcdrVersion, types.Semver),
				) {
					return &rel, nil
				}
				break
			}
		}
	}

	return nil, ErrVersionNotFound(id, "latest compatible")
}

func getReleaseHistory(id string) (*pluginRelease, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + id + "/release.json" + branchMeta
	err, msg, data := github.GetFileFromGitHub(ghEndpoint)
	if err != nil {
		return nil, err
	}
	if msg != nil && msg.Message != "" {
		if msg.Status == "404" {
			return nil, ErrPluginNotFound(id)
		}
		return nil, fmt.Errorf("%w: %s", ErrorGhApi, msg.Message)
	}

	var releaseHistory pluginRelease
	err = json.Unmarshal(data, &releaseHistory)
	if err != nil {
		return nil, err
	}
	return &releaseHistory, nil
}

func getRepository(id string) (*pluginRepo, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + id + "/repository.json" + branchMeta
	err, msg, data := github.GetFileFromGitHub(ghEndpoint)
	if err != nil {
		return nil, err
	}
	if msg != nil && msg.Message != "" {
		if msg.Status == "404" {
			return nil, ErrPluginNotFound(id)
		}
		return nil, fmt.Errorf("%w: %s", ErrorGhApi, msg.Message)
	}

	var repo pluginRepo
	err = json.Unmarshal(data, &repo)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func getFactory[T any]() func(id string) (*T, error) {
	return func(id string) (*T, error) {
		ghEndpoint := pluginCatalogueRepoEndpoint + id + "/repository.json" + branchMeta
		err, msg, data := github.GetFileFromGitHub(ghEndpoint)
		if err != nil {
			return nil, err
		}
		if msg != nil && msg.Message != "" {
			if msg.Status == "404" {
				return nil, ErrPluginNotFound(id)
			}
			return nil, fmt.Errorf("%w: %s", ErrorGhApi, msg.Message)
		}

		var res T
		err = json.Unmarshal(data, &res)
		if err != nil {
			return nil, err
		}
		return &res, nil
	}
}
