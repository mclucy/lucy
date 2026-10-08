package routing

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/providers/mojang"
)

type mavenLibrary struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Downloads struct {
		Artifact struct {
			Path string `json:"path"`
			URL  string `json:"url"`
			SHA1 string `json:"sha1"`
		} `json:"artifact"`
	} `json:"downloads"`
}

func libraryInput(lib mavenLibrary, z *zip.Reader, version string) (types.BootstrapInput, error) {
	p := lib.Downloads.Artifact.Path
	if p == "" {
		var err error
		p, err = MavenPath(lib.Name)
		if err != nil {
			return types.BootstrapInput{}, err
		}
	}
	a := types.Artifact{Provider: "maven", ProjectID: lib.Name, Version: version, Filename: path.Base(p), Hashes: map[string]string{}}
	if z != nil {
		if raw, err := zipBytes(z, "maven/"+p); err != nil {
			return types.BootstrapInput{}, err
		} else if raw != nil {
			a.Embedded = "maven/" + p
			sum := sha256.Sum256(raw)
			a.Hashes["sha256"] = hex.EncodeToString(sum[:])
			return types.BootstrapInput{Path: "libraries/" + p, Artifact: a}, nil
		}
	}
	a.URL = lib.Downloads.Artifact.URL
	if a.URL == "" {
		base := lib.URL
		if base == "" {
			base = "https://libraries.minecraft.net/"
		}
		a.URL = strings.TrimRight(base, "/") + "/" + p
	}
	if lib.Downloads.Artifact.SHA1 != "" {
		a.Hashes["sha1"] = lib.Downloads.Artifact.SHA1
	}
	return types.BootstrapInput{Path: "libraries/" + p, Artifact: a}, nil
}

func MavenPath(coordinate string) (string, error) {
	base, extension, has := strings.Cut(coordinate, "@")
	if !has {
		extension = "jar"
	}
	parts := strings.Split(base, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return "", fmt.Errorf("invalid Maven coordinate %q", coordinate)
	}
	name := parts[1] + "-" + parts[2]
	if len(parts) == 4 {
		name += "-" + parts[3]
	}
	return path.Join(strings.ReplaceAll(parts[0], ".", "/"), parts[1], parts[2], name+"."+extension), nil
}

func zipBytes(z *zip.Reader, name string) ([]byte, error) {
	for _, f := range z.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(io.LimitReader(r, 128<<20))
		}
	}
	return nil, nil
}

func resolveInstaller(ctx context.Context, req types.CoreRequest, out *types.CoreResolution, installer string) error {
	z, err := zip.OpenReader(installer)
	if err != nil {
		return err
	}
	defer z.Close()
	raw, err := zipBytes(&z.Reader, "install_profile.json")
	if err != nil {
		return err
	}
	if raw == nil {
		return fmt.Errorf("installer has no install_profile.json")
	}
	var profile struct {
		Minecraft string         `json:"minecraft"`
		Version   string         `json:"version"`
		JSON      string         `json:"json"`
		Libraries []mavenLibrary `json:"libraries"`
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		return err
	}
	if profile.Minecraft != req.Minecraft {
		return fmt.Errorf("%s installer targets Minecraft %s, requested %s", req.Distribution, profile.Minecraft, req.Minecraft)
	}
	out.Version = req.Version
	if floating(out.Version) {
		out.Version = strings.TrimSuffix(strings.TrimPrefix(out.Artifact.Filename, req.Distribution+"-"), "-installer.jar")
		if req.Distribution == "forge" {
			out.Version = strings.TrimPrefix(out.Version, req.Minecraft+"-")
		}
	}
	out.Artifact.Version = out.Version
	out.Bootstrap = "forge-installer"
	if profile.JSON != "" {
		raw, e := zipBytes(&z.Reader, strings.TrimPrefix(profile.JSON, "/"))
		if e != nil {
			return e
		}
		var detail struct {
			Libraries []mavenLibrary `json:"libraries"`
		}
		if e := json.Unmarshal(raw, &detail); e != nil {
			return e
		}
		profile.Libraries = append(profile.Libraries, detail.Libraries...)
	}
	seen := map[string]bool{}
	for _, lib := range profile.Libraries {
		input, e := libraryInput(lib, &z.Reader, out.Version)
		if e != nil {
			return e
		}
		if !seen[input.Path] {
			seen[input.Path] = true
			out.Inputs = append(out.Inputs, input)
		}
	}
	// Processors may need Mojang server mappings in addition to the server JAR.
	var index struct {
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := Metadata(ctx, mojang.VersionManifestURL, &index); err != nil {
		return err
	}
	for _, v := range index.Versions {
		if v.ID == req.Minecraft {
			var detail struct {
				Downloads map[string]struct {
					URL  string `json:"url"`
					SHA1 string `json:"sha1"`
				} `json:"downloads"`
			}
			if err := Metadata(ctx, v.URL, &detail); err != nil {
				return err
			}
			if mapping, ok := detail.Downloads["server_mappings"]; ok {
				out.Inputs = append(out.Inputs, types.BootstrapInput{Path: "server-mappings.txt", Artifact: types.Artifact{Provider: "mojang", ProjectID: "server-mappings", Version: req.Minecraft, Filename: "server-mappings.txt", URL: mapping.URL, Hashes: map[string]string{"sha1": mapping.SHA1}}})
			}
			break
		}
	}
	return nil
}
