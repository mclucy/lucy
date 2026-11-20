package routing

import (
	"archive/zip"
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

const (
	fillProjectBase = "https://fill.papermc.io/v3/projects/"
	fillUserAgent   = "lucy (+https://github.com/mclucy/lucy)"
)

// fillMetadata performs a Fill metadata request. Fill asks clients to identify
// themselves, while the router's generic Metadata helper sends no User-Agent;
// keep this narrowly scoped so other providers' request behavior is unchanged.
func (r *Router) fillMetadata(
	ctx context.Context,
	endpoint string,
	target any,
) error {
	res, err := r.fillMetadataResponse(ctx, endpoint)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("fetch failed: status %d", res.StatusCode)
	}
	return json.Unmarshal(res.Data, target)
}

func (r *Router) fillMetadataResponse(
	ctx context.Context,
	endpoint string,
) (*cache.BytesResponse, error) {
	r.logger.Debug("routing: metadata request", "endpoint", endpoint)
	headers := http.Header{}
	headers.Set("User-Agent", fillUserAgent)
	return r.network.CachedGetRequest(
		ctx,
		endpoint,
		cache.BytesRequestOptions{
			Kind:    cache.KindMetadata,
			Headers: headers,
		},
	)
}

// fillBuildsEndpoint builds a Fill v3 build-list URL. An empty channel keeps
// the unfiltered list; Fill's channel query is repeatable and only accepts its
// own channel enums.
func fillBuildsEndpoint(project, minecraft, channel string) string {
	endpoint := fillProjectBase + project + "/versions/" + minecraft + "/builds"
	if channel != "" {
		endpoint += "?channel=" + channel
	}
	return endpoint
}

// paperBuild is one build entry from the PaperMC fill API.
type paperBuild struct {
	ID        int    `json:"id"`
	Channel   string `json:"channel"`
	Downloads map[string]struct {
		Name      string            `json:"name"`
		URL       string            `json:"url"`
		Checksums map[string]string `json:"checksums"`
	} `json:"downloads"`
}

// IdentifyFillBuild matches a server jar digest against the published builds
// of a PaperMC fill project. Init uses it when a renamed jar no longer carries
// its build number.
func (r *Router) IdentifyFillBuild(
	ctx context.Context,
	project string,
	minecraft version.Version,
	sha256 string,
) (version.Version, bool, error) {
	// Fill has no digest lookup and GraphQL only moves the same build list
	// behind an experimental schema, so the version's build list is the
	// narrowest documented request for matching a jar's checksum.
	var builds []paperBuild
	endpoint := fillBuildsEndpoint(project, minecraft.String(), "")
	if err := r.fillMetadata(ctx, endpoint, &builds); err != nil {
		return "", false, err
	}
	for _, build := range builds {
		asset, ok := build.Downloads["server:default"]
		if !ok {
			continue
		}
		if strings.EqualFold(asset.Checksums["sha256"], sha256) {
			return version.Version(strconv.Itoa(build.ID)), true, nil
		}
	}
	return "", false, nil
}

// resolvePaper selects a Paper build for one Minecraft version. A pinned build
// uses Fill's single-build document, and a stable floating request filters the
// build list by channel. A channel-agnostic floating request keeps the full
// list: /builds/latest could name a build without a server download and skip an
// older usable one, and the channel rules only apply to the stable sentinel.
func (r *Router) resolvePaper(
	ctx context.Context,
	req upstream.ServerProductRequest,
	out *upstream.ServerProductResolution,
) error {
	if !floating(req.ProductVersion) {
		requested := req.ProductVersion.String()
		id, err := strconv.Atoi(requested)
		if err != nil || id < 0 || strconv.Itoa(id) != requested {
			return noPaperBuildError(req)
		}
		build, found, err := r.paperBuildByID(ctx, req.Minecraft.String(), id)
		if err != nil {
			return err
		}
		if !found || !r.adoptPaperBuild(req, out, build) {
			return noPaperBuildError(req)
		}
		return nil
	}

	channel := ""
	if req.ProductVersion == version.Stable {
		channel = "STABLE"
	}
	var builds []paperBuild
	endpoint := fillBuildsEndpoint("paper", req.Minecraft.String(), channel)
	if err := r.fillMetadata(ctx, endpoint, &builds); err != nil {
		return err
	}
	slices.SortFunc(builds, func(a, b paperBuild) int { return cmp.Compare(b.ID, a.ID) })
	for _, build := range builds {
		if req.ProductVersion == version.Stable && build.Channel != "STABLE" {
			r.logger.Debug(
				"routing: paper build skipped",
				"minecraft", req.Minecraft,
				"build", build.ID,
				"reason", "unstable build channel",
			)
			continue
		}
		if r.adoptPaperBuild(req, out, build) {
			return nil
		}
	}
	return noPaperBuildError(req)
}

// paperBuildByID reads one build document. Fill answers 404 for an unknown
// build; reading the status directly keeps that absence distinct from transport
// and cache failures.
func (r *Router) paperBuildByID(
	ctx context.Context,
	minecraft string,
	id int,
) (paperBuild, bool, error) {
	endpoint := fillBuildsEndpoint("paper", minecraft, "") + "/" + strconv.Itoa(id)
	res, err := r.fillMetadataResponse(ctx, endpoint)
	if err != nil {
		return paperBuild{}, false, err
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		return paperBuild{}, false, nil
	case res.StatusCode < 200 || res.StatusCode >= 300:
		return paperBuild{}, false, fmt.Errorf(
			"fetch failed: status %d",
			res.StatusCode,
		)
	}
	var build paperBuild
	if err := json.Unmarshal(res.Data, &build); err != nil {
		return paperBuild{}, false, err
	}
	return build, true, nil
}

// adoptPaperBuild records a candidate whose server download is published. It
// reports whether the build is usable so callers can keep scanning the list.
func (r *Router) adoptPaperBuild(
	req upstream.ServerProductRequest,
	out *upstream.ServerProductResolution,
	build paperBuild,
) bool {
	asset, ok := build.Downloads["server:default"]
	if !ok {
		r.logger.Debug(
			"routing: paper build skipped",
			"minecraft", req.Minecraft,
			"build", build.ID,
			"reason", "no server download",
		)
		return false
	}
	v := strconv.Itoa(build.ID)
	out.ProductVersion, out.Bootstrapper = version.Version(v), server.BootstrapMethodPaperclip
	out.Artifact = catalog.Artifact{
		Provider:  catalog.ProviderPaperMC,
		ProjectID: "paper",
		ReleaseID: req.Minecraft.String() + "/" + v,
		FileID:    "server:default",
		Version:   version.Version(v),
		Filename:  asset.Name,
		URL:       asset.URL,
		Hashes:    asset.Checksums,
	}
	r.logger.Debug(
		"routing: paper build selected",
		"minecraft", req.Minecraft,
		"build", v,
		"channel", build.Channel,
		"requested", req.ProductVersion,
	)
	return true
}

func noPaperBuildError(req upstream.ServerProductRequest) error {
	return fmt.Errorf("no Paper build %s for Minecraft %s", req.ProductVersion, req.Minecraft)
}

func (r *Router) resolveGithubProduct(
	ctx context.Context,
	req upstream.ServerProductRequest,
	out *upstream.ServerProductResolution,
	acquire func(*catalog.Artifact) (artifact.MaterializedFile, error),
) error {
	var repo struct {
		ID int64 `json:"id"`
	}
	if err := r.Metadata(ctx, "https://api.github.com/repos/"+req.Repository, &repo); err != nil {
		return err
	}
	var releases []struct {
		ID         int64  `json:"id"`
		Tag        string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
		Assets     []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := r.Metadata(ctx, "https://api.github.com/repos/"+req.Repository+"/releases?per_page=100", &releases); err != nil {
		return err
	}
	for _, rl := range releases {
		reason := ""
		switch {
		case rl.Draft:
			reason = "draft release"
		case !floating(req.ProductVersion) && rl.Tag != req.ProductVersion.String():
			reason = "release tag mismatch"
		case req.ProductVersion == version.Stable && rl.Prerelease:
			reason = "prerelease excluded"
		}
		if reason != "" {
			r.logger.Debug(
				"routing: github release skipped",
				"repository", req.Repository,
				"release", rl.Tag,
				"reason", reason,
			)
			continue
		}
		for _, a := range rl.Assets {
			reason := ""
			if !strings.HasSuffix(a.Name, ".jar") {
				reason = "not a jar asset"
			} else if req.FilePattern != "" {
				matched, e := filepath.Match(req.FilePattern, a.Name)
				if e != nil {
					return e
				}
				if !matched {
					reason = "file pattern mismatch"
				}
			}
			if reason != "" {
				r.logger.Debug(
					"routing: github release asset skipped",
					"repository", req.Repository,
					"release", rl.Tag,
					"asset", a.Name,
					"reason", reason,
				)
				continue
			}
			hashes := map[string]string{}
			if h, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
				hashes["sha256"] = h
			}
			candidate := catalog.Artifact{
				Provider:  catalog.ProviderGitHub,
				ProjectID: strconv.FormatInt(repo.ID, 10),
				ReleaseID: strconv.FormatInt(rl.ID, 10),
				FileID:    strconv.FormatInt(a.ID, 10),
				Version:   version.Version(rl.Tag),
				Filename:  a.Name,
				URL:       a.URL,
				Hashes:    hashes,
			}
			trial := *out
			trial.ProductVersion = version.Version(rl.Tag)
			trial.Bootstrapper = server.BootstrapMethodPaperclip
			materialized, e := acquire(&candidate)
			if e != nil {
				return e
			}
			candidate.Hashes = materialized.Hashes
			trial.Artifact = candidate
			if e := paperclipInput(&trial, materialized.Path); e != nil {
				r.logger.Debug(
					"routing: github fork candidate rejected",
					"repository", req.Repository,
					"release", rl.Tag,
					"asset", a.Name,
					"reason", "paperclip download context unusable",
				)
				continue
			}
			*out = trial
			r.logger.Debug(
				"routing: github fork core selected",
				"repository", req.Repository,
				"release", rl.Tag,
				"asset", a.Name,
			)
			return nil
		}
	}
	return fmt.Errorf("no GitHub Paper fork artifact for Minecraft %s in %s", req.Minecraft, req.Repository)
}

func paperclipInput(out *upstream.ServerProductResolution, corePath string) error {
	z, err := zip.OpenReader(corePath)
	if err != nil {
		return err
	}
	defer z.Close()
	data, err := zipBytes(&z.Reader, "META-INF/download-context")
	if err != nil {
		return err
	}
	if data == nil {
		return fmt.Errorf("paper fork has no supported Paperclip download context")
	}
	fields := strings.Fields(string(data))
	if len(fields) != 3 {
		return fmt.Errorf("invalid Paperclip download context")
	}
	expected := "mojang_" + out.Minecraft.String() + ".jar"
	if fields[2] != expected {
		return fmt.Errorf("core targets %s, requested Minecraft %s", fields[2], out.Minecraft)
	}
	out.Inputs = []server.BootstrapInput{
		{
			Path:      path.Join("cache", fields[2]),
			Provider:  catalog.ProviderMojang,
			ProjectID: "minecraft",
			Version:   out.Minecraft,
			Filename:  fields[2],
			URL:       fields[1],
			Hashes:    map[string]string{"sha256": fields[0]},
		},
	}
	return nil
}
