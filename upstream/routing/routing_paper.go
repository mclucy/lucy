package routing

import (
	"archive/zip"
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

func resolvePaper(ctx context.Context, req types.CoreRequest, out *types.CoreResolution) error {
	var builds []struct {
		ID        int    `json:"id"`
		Channel   string `json:"channel"`
		Downloads map[string]struct {
			Name      string            `json:"name"`
			URL       string            `json:"url"`
			Checksums map[string]string `json:"checksums"`
		} `json:"downloads"`
	}
	endpoint := "https://fill.papermc.io/v3/projects/paper/versions/" + req.Minecraft + "/builds"
	if err := Metadata(ctx, endpoint, &builds); err != nil {
		return err
	}
	sort.Slice(builds, func(i, j int) bool { return builds[i].ID > builds[j].ID })
	for _, build := range builds {
		v := strconv.Itoa(build.ID)
		reason := ""
		switch {
		case !floating(req.Version) && v != req.Version:
			reason = "build version mismatch"
		case req.Version == "stable" && build.Channel != "STABLE":
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
		out.Version, out.Bootstrap = v, "paperclip"
		out.Artifact = types.Artifact{Provider: "papermc", ProjectID: "paper", ReleaseID: req.Minecraft + "/" + v, FileID: "server:default", Version: v, Filename: asset.Name, URL: asset.URL, Hashes: asset.Checksums}
		log.Debug(
			"routing: paper build selected",
			"minecraft", req.Minecraft,
			"build", v,
			"channel", build.Channel,
			"requested", req.Version,
		)
		return nil
	}
	return fmt.Errorf("no Paper build %s for Minecraft %s", req.Version, req.Minecraft)
}

func resolveGithubCore(ctx context.Context, req types.CoreRequest, out *types.CoreResolution, acquire func(*types.Artifact) (string, error)) error {
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
		case !floating(req.Version) && r.Tag != req.Version:
			reason = "release tag mismatch"
		case req.Version == "stable" && r.Prerelease:
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
			} else if req.File != "" {
				matched, e := filepath.Match(req.File, a.Name)
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
			candidate := types.Artifact{Provider: "github", ProjectID: strconv.FormatInt(repo.ID, 10), ReleaseID: strconv.FormatInt(r.ID, 10), FileID: strconv.FormatInt(a.ID, 10), Version: r.Tag, Filename: a.Name, URL: a.URL, Hashes: hashes}
			p, e := acquire(&candidate)
			if e != nil {
				return e
			}
			trial := *out
			trial.Artifact = candidate
			trial.Version = r.Tag
			trial.Bootstrap = "paperclip"
			if e := paperclipInput(&trial, p); e != nil {
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

func paperclipInput(out *types.CoreResolution, corePath string) error {
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
		return fmt.Errorf("Paper fork has no supported Paperclip download context")
	}
	fields := strings.Fields(string(data))
	if len(fields) != 3 {
		return fmt.Errorf("invalid Paperclip download context")
	}
	expected := "mojang_" + out.Minecraft + ".jar"
	if fields[2] != expected {
		return fmt.Errorf("core targets %s, requested Minecraft %s", fields[2], out.Minecraft)
	}
	out.Inputs = []types.BootstrapInput{{Path: path.Join("cache", fields[2]), Artifact: types.Artifact{Provider: "mojang", ProjectID: "minecraft", Version: out.Minecraft, Filename: fields[2], URL: fields[1], Hashes: map[string]string{"sha256": fields[0]}}}}
	return nil
}
