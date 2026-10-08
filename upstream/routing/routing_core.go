package routing

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/fabric"
	"github.com/mclucy/lucy/upstream/providers/forge"
	"github.com/mclucy/lucy/upstream/providers/mojang"
	"github.com/mclucy/lucy/upstream/providers/neoforge"
)

func Metadata(ctx context.Context, endpoint string, target any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := cache.CachedGetBytes(endpoint, cache.BytesRequestOptions{Kind: cache.KindMetadata})
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func ResolveCore(ctx context.Context, req types.CoreRequest, acquire func(*types.Artifact) (string, error)) (types.CoreResolution, error) {
	out := types.CoreResolution{Minecraft: req.Minecraft, Distribution: req.Distribution, Java: 21}
	if strings.HasPrefix(req.Minecraft, "1.") {
		parts := strings.Split(req.Minecraft, ".")
		if len(parts) > 1 {
			minor, _ := strconv.Atoi(parts[1])
			if minor < 17 {
				out.Java = 8
			} else if minor < 18 {
				out.Java = 16
			} else if minor < 20 || (minor == 20 && len(parts) > 2 && parts[2] < "5") {
				out.Java = 17
			}
		}
	} else {
		out.Java = 25
	}
	local := upstream.LocalContext{ModLoader: types.EcoVanilla, GameVersion: types.BareVersion(req.Minecraft)}
	vanilla, err := mojang.Provider.Fetch(local, types.VersionedPackageRef{Name: "minecraft", Eco: types.EcoMinecraft, Version: types.BareVersion(req.Minecraft)})
	if err != nil {
		return out, err
	}
	minecraft := types.Artifact{Provider: "mojang", ProjectID: "minecraft", Version: req.Minecraft, Filename: "server.jar", URL: vanilla.FileUrl, Hashes: map[string]string{"sha1": vanilla.Hash}}
	if req.Distribution == "vanilla" {
		out.Artifact = minecraft
		out.Version = req.Minecraft
		_, err = acquire(&out.Artifact)
		return out, err
	}
	out.Inputs = []types.BootstrapInput{{Path: "server.jar", Artifact: minecraft}}
	if req.Repository != "" {
		err = resolveGithubCore(ctx, req, &out, acquire)
	} else {
		switch req.Distribution {
		case "fabric":
			selected, e := fabric.Provider.ResolveVersionSelector(local, types.VersionedPackageRef{Name: "fabric", Eco: types.EcoFabric, Version: types.BareVersion(req.Version)})
			if e != nil {
				return out, e
			}
			out.Version = string(selected.Version)
			out.Bootstrap = "fabric"
			var profile struct {
				MainClass string         `json:"mainClass"`
				Libraries []mavenLibrary `json:"libraries"`
			}
			if e := Metadata(ctx, "https://meta.fabricmc.net/v2/versions/loader/"+url.PathEscape(req.Minecraft)+"/"+url.PathEscape(out.Version)+"/server/json", &profile); e != nil {
				return out, e
			}
			out.Artifact = types.Artifact{Provider: "fabric", ProjectID: "fabric-loader", Version: out.Version, Filename: "fabric-loader-" + out.Version + ".jar", URL: "https://maven.fabricmc.net/net/fabricmc/fabric-loader/" + out.Version + "/fabric-loader-" + out.Version + ".jar", Hashes: map[string]string{}}
			_, err = acquire(&out.Artifact)
			if err != nil {
				return out, err
			}
			for _, lib := range profile.Libraries {
				input, e := libraryInput(lib, nil, out.Version)
				if e != nil {
					return out, e
				}
				if input.Artifact.URL == out.Artifact.URL {
					continue
				}
				out.Inputs = append(out.Inputs, input)
			}
		case "forge", "neoforge":
			var remote types.ResolvedPackage
			if req.Distribution == "forge" {
				remote, err = forge.Provider.Fetch(local, types.VersionedPackageRef{Name: "forge", Eco: types.EcoForge, Version: types.BareVersion(req.Version)})
			} else {
				remote, err = neoforge.Provider.Fetch(local, types.VersionedPackageRef{Name: "neoforge", Eco: types.EcoNeoforge, Version: types.BareVersion(req.Version)})
			}
			if err != nil {
				return out, err
			}
			out.Artifact = types.Artifact{Provider: req.Distribution, ProjectID: req.Distribution, Version: req.Version, Filename: remote.Filename, URL: remote.FileUrl, Hashes: map[string]string{}}
			installer, e := acquire(&out.Artifact)
			if e != nil {
				return out, e
			}
			err = resolveInstaller(ctx, req, &out, installer)
		case "paper":
			err = resolvePaper(ctx, req, &out)
		case "purpur":
			build := req.Version
			if floating(build) {
				build = "latest"
			}
			var info struct {
				Build   string `json:"build"`
				Version string `json:"version"`
				MD5     string `json:"md5"`
			}
			if e := Metadata(ctx, "https://api.purpurmc.org/v2/purpur/"+req.Minecraft+"/"+build, &info); e != nil {
				return out, e
			}
			if info.Version != req.Minecraft {
				return out, fmt.Errorf("Purpur release targets Minecraft %s, requested %s", info.Version, req.Minecraft)
			}
			out.Version = info.Build
			out.Artifact = types.Artifact{Provider: "purpur", ProjectID: "purpur", ReleaseID: info.Build, Version: info.Build, Filename: "purpur-" + req.Minecraft + "-" + info.Build + ".jar", URL: "https://api.purpurmc.org/v2/purpur/" + req.Minecraft + "/" + info.Build + "/download", Hashes: map[string]string{"md5": info.MD5}}
			out.Bootstrap = "paperclip"
		default:
			return out, fmt.Errorf("unsupported distribution %q", req.Distribution)
		}
	}
	if err != nil {
		return out, err
	}
	corePath, err := acquire(&out.Artifact)
	if err != nil {
		return out, err
	}
	if out.Bootstrap == "paperclip" {
		if err := paperclipInput(&out, corePath); err != nil {
			return out, err
		}
	}
	for i := range out.Inputs {
		if out.Inputs[i].Embedded == "" {
			if _, e := acquire(&out.Inputs[i].Artifact); e != nil {
				return out, e
			}
		}
	}
	return out, nil
}

func floating(v string) bool {
	return v == "" || v == "stable" || v == "any" || v == "beta" || v == "latest"
}
