package routing

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/log"
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
	log.Debug("routing: metadata request", "endpoint", endpoint)
	data, err := cache.CachedGetBytes(endpoint, cache.BytesRequestOptions{Kind: cache.KindMetadata})
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// trustMavenChecksum adopts the digest the artifact's Maven repository
// publishes beside it. Acquisition records whatever digest it computes when
// nothing is supplied to compare against, so a core artifact without a trusted
// checksum would pin unverified bytes in the lock. The repository sidecar is
// that source, and an unavailable one fails the resolution rather than
// lowering the guarantee.
func trustMavenChecksum(ctx context.Context, a *types.Artifact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := cache.CachedGetBytes(a.URL+".sha256", cache.BytesRequestOptions{Kind: cache.KindMetadata})
	if err != nil {
		return fmt.Errorf("no trusted checksum for %s: %w", a.Filename, err)
	}
	sum := strings.TrimSpace(string(data))
	if len(sum) != 64 {
		return fmt.Errorf("malformed checksum for %s", a.Filename)
	}
	a.Hashes["sha256"] = sum
	log.Debug(
		"routing: trusted checksum adopted",
		"artifact", a.Filename,
		"sha256", sum,
	)
	return nil
}

// ResolveServerProduct resolves the server product artifact, its download
// inputs, and the Java requirement for the requested Minecraft version.
func ResolveServerProduct(
	ctx context.Context,
	req types.ServerProductRequest,
	acquire func(*types.Artifact) (string, error),
) (types.ServerProductResolution, error) {
	out := types.ServerProductResolution{
		Minecraft:      req.Minecraft,
		Product:        req.Product,
		ProductVersion: req.ProductVersion,
		Java:           21,
	}
	if strings.HasPrefix(req.Minecraft.String(), "1.") {
		// A prerelease or release-candidate suffix rides on the Minecraft id
		// ("1.20-pre1", "1.20.5-pre1"). The numeric parts alone decide the Java
		// requirement, and a suffix on any of them silently parses as zero,
		// which would map the 1.20 prereleases to Java 8.
		parts := strings.Split(
			strings.SplitN(req.Minecraft.String(), "-", 2)[0],
			".",
		)
		if len(parts) > 1 {
			minor, _ := strconv.Atoi(parts[1])
			// An absent patch is the unpatched release, and 1.20 predates the
			// Java 21 requirement that 1.20.5 introduced.
			patch := 0
			if len(parts) > 2 {
				patch, _ = strconv.Atoi(parts[2])
			}
			if minor < 17 {
				out.Java = 8
			} else if minor < 18 {
				out.Java = 16
			} else if minor < 20 || (minor == 20 && patch < 5) {
				out.Java = 17
			}
		}
	} else {
		out.Java = 25
	}
	local := upstream.LocalContext{
		Ecosystem:        types.EcosystemMinecraft,
		MinecraftVersion: req.Minecraft,
	}
	vanilla, err := mojang.Provider.Fetch(
		local,
		types.VersionedPackageRef{
			Name:      "minecraft",
			Ecosystem: types.EcosystemMinecraft,
			Version:   req.Minecraft,
		},
	)
	if err != nil {
		return out, err
	}
	minecraft := types.Artifact{
		Provider:  types.ProviderMojang,
		ProjectID: "minecraft",
		Version:   req.Minecraft,
		Filename:  "server.jar",
		URL:       vanilla.FileURL,
		Hashes:    map[string]string{"sha1": vanilla.Hash},
	}
	if req.Product == types.ServerProductMinecraft {
		log.Debug(
			"routing: server product route selected",
			"product", req.Product,
			"provider", types.ProviderMojang,
		)
		out.Artifact = minecraft
		out.ProductVersion = req.Minecraft
		_, err = acquire(&out.Artifact)
		return out, err
	}
	out.Inputs = []types.BootstrapInput{
		{
			Path:     "server.jar",
			Artifact: minecraft,
		},
	}
	if req.Repository != "" {
		log.Debug(
			"routing: server product route selected",
			"product", req.Product,
			"provider", types.ProviderGitHub,
			"repository", req.Repository,
		)
		err = resolveGithubProduct(ctx, req, &out, acquire)
	} else {
		switch req.Product {
		case types.ServerProductFabric:
			selected, e := fabric.Provider.ResolveVersionSelector(
				local,
				types.VersionedPackageRef{
					Name:      "fabric",
					Ecosystem: types.EcosystemFabric,
					Version:   req.ProductVersion,
				},
			)
			if e != nil {
				return out, e
			}
			out.ProductVersion = selected.Version
			out.Bootstrapper = "fabric"
			log.Debug(
				"routing: server product route selected",
				"product", req.Product,
				"provider", types.ProviderFabric,
				"version", out.ProductVersion,
			)
			var profile struct {
				MainClass string         `json:"mainClass"`
				Libraries []mavenLibrary `json:"libraries"`
			}
			if e := Metadata(
				ctx,
				"https://meta.fabricmc.net/v2/versions/loader/"+
					url.PathEscape(req.Minecraft.String())+"/"+
					url.PathEscape(out.ProductVersion.String())+
					"/server/json",
				&profile,
			); e != nil {
				return out, e
			}
			out.Artifact = types.Artifact{
				Provider:  types.ProviderFabric,
				ProjectID: "fabric-loader",
				Version:   out.ProductVersion,
				Filename:  "fabric-loader-" + out.ProductVersion.String() + ".jar",
				URL: "https://maven.fabricmc.net/net/fabricmc/fabric-loader/" +
					out.ProductVersion.String() + "/fabric-loader-" +
					out.ProductVersion.String() + ".jar",
				Hashes: map[string]string{},
			}
			if e := trustMavenChecksum(ctx, &out.Artifact); e != nil {
				return out, e
			}
			_, err = acquire(&out.Artifact)
			if err != nil {
				return out, err
			}
			for _, lib := range profile.Libraries {
				input, downloadable, e := libraryInput(
					lib,
					nil,
					out.ProductVersion,
				)
				if e != nil {
					return out, e
				}
				if !downloadable || input.Artifact.URL == out.Artifact.URL {
					continue
				}
				out.Inputs = append(out.Inputs, input)
			}
		case types.ServerProductForge, types.ServerProductNeoForge:
			var (
				remote   types.ResolvedPackage
				provider types.Provider
			)
			if req.Product == types.ServerProductForge {
				provider = types.ProviderForge
				remote, err = forge.Provider.Fetch(
					local,
					types.VersionedPackageRef{
						Name:      "forge",
						Ecosystem: types.EcosystemForge,
						Version:   req.ProductVersion,
					},
				)
			} else {
				provider = types.ProviderNeoForge
				remote, err = neoforge.Provider.Fetch(
					local,
					types.VersionedPackageRef{
						Name:      "neoforge",
						Ecosystem: types.EcosystemNeoForge,
						Version:   req.ProductVersion,
					},
				)
			}
			if err != nil {
				return out, err
			}
			out.Artifact = types.Artifact{
				Provider:  provider,
				ProjectID: req.Product.String(),
				Version:   req.ProductVersion,
				Filename:  remote.Filename,
				URL:       remote.FileURL,
				Hashes:    map[string]string{},
			}
			installer, e := acquire(&out.Artifact)
			if e != nil {
				return out, e
			}
			err = resolveInstaller(ctx, req, &out, installer)
			if err != nil {
				return out, err
			}
			log.Debug(
				"routing: server product route selected",
				"product", req.Product,
				"provider", provider,
				"version", out.ProductVersion,
			)
		case types.ServerProductPaper:
			err = resolvePaper(ctx, req, &out)
		case types.ServerProductPurpur:
			build := req.ProductVersion
			if floating(build) {
				build = "latest"
			}
			var info struct {
				Build   string `json:"build"`
				Version string `json:"version"`
				MD5     string `json:"md5"`
			}
			if e := Metadata(
				ctx,
				"https://api.purpurmc.org/v2/purpur/"+
					req.Minecraft.String()+"/"+build.String(),
				&info,
			); e != nil {
				return out, e
			}
			if info.Version != req.Minecraft.String() {
				return out, fmt.Errorf(
					"purpur release targets Minecraft %s, requested %s",
					info.Version,
					req.Minecraft,
				)
			}
			out.ProductVersion = types.Version(info.Build)
			out.Artifact = types.Artifact{
				Provider:  types.ProviderPurpur,
				ProjectID: "purpur",
				ReleaseID: info.Build,
				Version:   types.Version(info.Build),
				Filename: "purpur-" + req.Minecraft.String() + "-" +
					info.Build + ".jar",
				URL: "https://api.purpurmc.org/v2/purpur/" +
					req.Minecraft.String() + "/" + info.Build + "/download",
				Hashes: map[string]string{"md5": info.MD5},
			}
			out.Bootstrapper = "paperclip"
			log.Debug(
				"routing: server product route selected",
				"product", req.Product,
				"provider", types.ProviderPurpur,
				"build", info.Build,
			)
		default:
			log.Debug(
				"routing: server product route rejected",
				"product", req.Product,
				"reason", "unsupported server product",
			)
			return out, fmt.Errorf("unsupported server product %q", req.Product)
		}
	}
	if err != nil {
		return out, err
	}
	corePath, err := acquire(&out.Artifact)
	if err != nil {
		return out, err
	}
	if out.Bootstrapper == "paperclip" {
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

func floating(v types.Version) bool {
	switch v {
	case "", types.VersionAny, types.VersionBeta, types.VersionStable, "latest":
		return true
	default:
		return false
	}
}
