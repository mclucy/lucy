package mcdr

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
	"github.com/mclucy/lucy/cache"
)

const (
	pluginCatalogueRepoEndpoint = `https://api.github.com/repos/MCDReforged/PluginCatalogue/contents/`
	branchCatalogue             = "?ref=catalogue"

	mcdrCatalogueEndpoint = `https://api.mcdreforged.com/catalogue/`
)

// search lists plugin ids through the GitHub contents API. The official
// plugins.json carries the same ids in about 1.35 MB for one request, so this
// listing is kept while it stays far below the contents API's 1000-entry cap;
// switching would buy no request reduction and only grow the payload.
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

	return fuzzySearch(query, pluginIds), nil
}

var initFuzzyScoring sync.Once

// fuzzySearch ranks ids by how well they match query.
func fuzzySearch(query string, ids []string) []string {
	if query == "" {
		return nil
	}
	initFuzzyScoring.Do(func() { algo.Init("default") })
	pattern := []rune(strings.ToLower(query))
	slab := util.MakeSlab(16, 32)

	type ranked struct {
		index int
		score int
	}
	matches := make([]ranked, 0, len(ids))
	for i, id := range ids {
		chars := util.ToChars([]byte(id))
		result, _ := algo.FuzzyMatchV2(
			false, true, true, &chars, pattern, false, slab,
		)
		if result.Score > 0 {
			matches = append(matches, ranked{i, result.Score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = append(result, ids[match.index])
	}
	return result
}

// getPlugin fetches one plugin's aggregate catalogue document. Its meta,
// plugin, and repository sections replace the three former GitHub contents
// requests with a single request. Sections the catalogue failed to refresh are
// null and stay absent from the resulting metadata.
func (p *Provider) getPlugin(
	ctx context.Context,
	id string,
) (*pluginAll, error) {
	var all pluginAll
	if err := p.getPluginDocument(ctx, id, "all.json", &all); err != nil {
		return nil, err
	}
	if all.Meta == nil && all.Plugin == nil && all.Repository == nil {
		return nil, fmt.Errorf("%w: %s has no catalogue data", ErrorCatalogueApi, id)
	}
	return &all, nil
}

// getReleaseHistory fetches the plugin's narrow release document. It carries
// the same release history as the catalogue aggregate without the plugin's
// readme and introduction.
func (p *Provider) getReleaseHistory(
	ctx context.Context,
	id string,
) (*pluginRelease, error) {
	var releaseHistory pluginRelease
	if err := p.getPluginDocument(ctx, id, "release.json", &releaseHistory); err != nil {
		return nil, err
	}
	return &releaseHistory, nil
}

// getAuthorLinks fetches the catalogue-wide author index. Author links are the
// one field the aggregate drops from plugin_info.json, and the index is
// shared by every plugin and cached under its own URL, so it is requested only
// when a plugin lists authors and reused across plugins.
func (p *Provider) getAuthorLinks(
	ctx context.Context,
) (map[string]string, error) {
	var summary authorSummary
	if err := p.getCatalogueJSON(ctx, mcdrCatalogueEndpoint+"authors.json", &summary); err != nil {
		return nil, err
	}
	links := make(map[string]string, len(summary.Authors))
	for name, record := range summary.Authors {
		if record.Link != "" {
			links[name] = record.Link
		}
	}
	return links, nil
}

func (p *Provider) getPluginDocument(
	ctx context.Context,
	id string,
	document string,
	out any,
) error {
	rawURL := mcdrCatalogueEndpoint + url.PathEscape(id) + "/" + document
	res, err := p.fetchCatalogue(ctx, rawURL)
	if err != nil {
		return err
	}
	switch res.StatusCode {
	case http.StatusOK:
		return decodeCatalogue(res.Data, out)
	case http.StatusNotFound:
		return ErrPluginNotFound(id)
	default:
		return fmt.Errorf("%w: unexpected status %d", ErrorCatalogueApi, res.StatusCode)
	}
}

func (p *Provider) getCatalogueJSON(
	ctx context.Context,
	rawURL string,
	out any,
) error {
	res, err := p.fetchCatalogue(ctx, rawURL)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: unexpected status %d", ErrorCatalogueApi, res.StatusCode)
	}
	return decodeCatalogue(res.Data, out)
}

func (p *Provider) fetchCatalogue(
	ctx context.Context,
	rawURL string,
) (*cache.BytesResponse, error) {
	p.logger.Debug("mcdr catalogue: GET " + rawURL)
	res, err := p.network.CachedGetRequest(
		ctx,
		rawURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrorCatalogueApi, err)
	}
	return res, nil
}

func decodeCatalogue(data []byte, out any) error {
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %w", ErrorCatalogueApi, err)
	}
	return nil
}
