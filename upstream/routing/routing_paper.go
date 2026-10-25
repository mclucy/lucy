package routing

import (
	"archive/zip"
	"cmp"
	"context"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

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
func IdentifyFillBuild(
	ctx context.Context,
	project string,
	minecraft version.Version,
	sha256 string,
) (version.Version, bool, error) {
	var builds []paperBuild
	endpoint := "https://fill.papermc.io/v3/projects/" + project +
		"/versions/" + minecraft.String() + "/builds"
	if err := Metadata(ctx, endpoint, &builds); err != nil {
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

func resolvePaper(ctx context.Context, req ServerProductRequest, out *ServerProductResolution) error {
	var builds []paperBuild
	endpoint := "https://fill.papermc.io/v3/projects/paper/versions/" + req.Minecraft.String() + "/builds"
	if err := Metadata(ctx, endpoint, &builds); err != nil {
		return err
	}
	slices.SortFunc(builds, func(a, b paperBuild) int { return cmp.Compare(b.ID, a.ID) })
	for _, build := range builds {
		v := strconv.Itoa(build.ID)
		reason := ""
		switch {
		case !floating(req.ProductVersion) && v != req.ProductVersion.String():
			reason = "build version mismatch"
		case req.ProductVersion == version.Stable && build.Channel != "STABLE":
			reason = "unstable build channel"
		}
		asset, ok := build.Downloads["server:default"]
		if !ok && reason == "" {
			reason = "no server download"
		}
		if reason != "" {
			log.Debug(
				"routing: paper build skipped",
				"minecraft", req.Minecraft,
				"build", v,
				"reason", reason,
			)
			continue
		}
		out.ProductVersion, out.Bootstrapper = version.Version(v), "paperclip"
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
		log.Debug(
			"routing: paper build selected",
			"minecraft", req.Minecraft,
			"build", v,
			"channel", build.Channel,
			"requested", req.ProductVersion,
		)
		return nil
	}
	return fmt.Errorf("no Paper build %s for Minecraft %s", req.ProductVersion, req.Minecraft)
}

func resolveGithubProduct(ctx context.Context, req ServerProductRequest, out *ServerProductResolution, acquire func(*catalog.Artifact) (artifact.MaterializedFile, error)) error {
	var repo struct {
		ID int64 `json:"id"`
	}
	if err := Metadata(ctx, "https://api.github.com/repos/"+req.Repository, &repo); err != nil {
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
	if err := Metadata(ctx, "https://api.github.com/repos/"+req.Repository+"/releases?per_page=100", &releases); err != nil {
		return err
	}
	for _, r := range releases {
		reason := ""
		switch {
		case r.Draft:
			reason = "draft release"
		case !floating(req.ProductVersion) && r.Tag != req.ProductVersion.String():
			reason = "release tag mismatch"
		case req.ProductVersion == version.Stable && r.Prerelease:
			reason = "prerelease excluded"
		}
		if reason != "" {
			log.Debug(
				"routing: github release skipped",
				"repository", req.Repository,
				"release", r.Tag,
				"reason", reason,
			)
			continue
		}
		for _, a := range r.Assets {
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
				log.Debug(
					"routing: github release asset skipped",
					"repository", req.Repository,
					"release", r.Tag,
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
				ReleaseID: strconv.FormatInt(r.ID, 10),
				FileID:    strconv.FormatInt(a.ID, 10),
				Version:   version.Version(r.Tag),
				Filename:  a.Name,
				URL:       a.URL,
				Hashes:    hashes,
			}
			trial := *out
			trial.ProductVersion = version.Version(r.Tag)
			trial.Bootstrapper = "paperclip"
			materialized, e := acquire(&candidate)
			if e != nil {
				return e
			}
			candidate.Hashes = materialized.Hashes
			trial.Artifact = candidate
			if e := paperclipInput(&trial, materialized.Path); e != nil {
				log.Debug(
					"routing: github fork candidate rejected",
					"repository", req.Repository,
					"release", r.Tag,
					"asset", a.Name,
					"reason", "paperclip download context unusable",
				)
				continue
			}
			*out = trial
			log.Debug(
				"routing: github fork core selected",
				"repository", req.Repository,
				"release", r.Tag,
				"asset", a.Name,
			)
			return nil
		}
	}
	return fmt.Errorf("no GitHub Paper fork artifact for Minecraft %s in %s", req.Minecraft, req.Repository)
}

func paperclipInput(out *ServerProductResolution, corePath string) error {
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
