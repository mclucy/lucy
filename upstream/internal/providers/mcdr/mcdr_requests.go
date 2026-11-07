package mcdr

import (
	"context"
	"encoding/json/v2"
	"fmt"

	"github.com/sahilm/fuzzy"
)

const (
	pluginCatalogueRepoEndpoint = `https://api.github.com/repos/MCDReforged/PluginCatalogue/contents/`
	branchMaster                = "?ref=master"
	branchCatalogue             = "?ref=catalogue"
	branchMeta                  = "?ref=meta"
)

func (p *Provider) search(
	ctx context.Context,
	query string,
) (mcdrSearchResult, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + "plugins/" + branchCatalogue
	err, msg, items := p.githubClient.GetDirectoryFromGitHub(ctx, ghEndpoint)
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

func (p *Provider) getInfo(
	ctx context.Context,
	id string,
) (*pluginInfo, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + "plugins/" + id + "/plugin_info.json" + branchMaster
	var data []byte
	err, msg, data := p.githubClient.GetFileFromGitHub(ctx, ghEndpoint)
	if err != nil {
		return nil, err
	}
	if msg != nil && msg.Message != "" {
		if msg.Status == "404" {
			return nil, ErrPluginNotFound(id)
		}
		return nil, fmt.Errorf("%w: %s", ErrorGhApi, msg.Message)
	}

	var info pluginInfo
	err = json.Unmarshal(data, &info)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (p *Provider) getMeta(
	ctx context.Context,
	id string,
) (*pluginMeta, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + id + "/meta.json" + branchMeta
	err, msg, data := p.githubClient.GetFileFromGitHub(ctx, ghEndpoint)
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

func (p *Provider) getReleaseHistory(
	ctx context.Context,
	id string,
) (*pluginRelease, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + id + "/release.json" + branchMeta
	err, msg, data := p.githubClient.GetFileFromGitHub(ctx, ghEndpoint)
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

func (p *Provider) getRepository(
	ctx context.Context,
	id string,
) (*pluginRepo, error) {
	ghEndpoint := pluginCatalogueRepoEndpoint + id + "/repository.json" + branchMeta
	err, msg, data := p.githubClient.GetFileFromGitHub(ctx, ghEndpoint)
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
