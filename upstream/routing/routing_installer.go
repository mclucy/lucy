package routing

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"path"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/server"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream/providers/mojang"
	versionpkg "github.com/mclucy/lucy/version"
)

type mavenLibrary struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Downloads struct {
		Artifact *struct {
			Path string `json:"path"`
			URL  string `json:"url"`
			SHA1 string `json:"sha1"`
		} `json:"artifact"`
	} `json:"downloads"`
}

// libraryInput turns one profile library into a bootstrap input. The second
// result is false when the profile explicitly names a library that cannot be
// downloaded: Forge declares the client classifier with an empty artifact URL
// because the jar exists nowhere in a public Maven repository.
func libraryInput(lib mavenLibrary, z *zip.Reader, version versionpkg.Version) (server.BootstrapInput, bool, error) {
	download := lib.Downloads.Artifact
	p := ""
	if download != nil {
		p = download.Path
	}
	if p == "" {
		var err error
		p, err = MavenPath(lib.Name)
		if err != nil {
			return server.BootstrapInput{}, false, err
		}
	}
	a := catalog.Artifact{Provider: catalog.ProviderMaven, ProjectID: lib.Name, Version: version, Filename: path.Base(p), Hashes: map[string]string{}}
	if z != nil {
		if raw, err := zipBytes(z, "maven/"+p); err != nil {
			return server.BootstrapInput{}, false, err
		} else if raw != nil {
			a.Embedded = "maven/" + p
			sum := sha256.Sum256(raw)
			a.Hashes["sha256"] = hex.EncodeToString(sum[:])
			return server.BootstrapInput{Path: "libraries/" + p, Artifact: a}, true, nil
		}
	}
	if download != nil && download.URL == "" {
		return server.BootstrapInput{}, false, nil
	}
	if download != nil {
		a.URL = download.URL
	}
	if a.URL == "" {
		base := lib.URL
		if base == "" {
			base = "https://libraries.minecraft.net/"
		}
		a.URL = strings.TrimRight(base, "/") + "/" + p
	}
	if download != nil && download.SHA1 != "" {
		a.Hashes["sha1"] = download.SHA1
	}
	return server.BootstrapInput{Path: "libraries/" + p, Artifact: a}, true, nil
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
	raw, _, err := fsutil.ReadZipEntry(
		z,
		name,
		fsutil.MaxNestedZipEntryBytes,
	)
	return raw, err
}

func resolveInstaller(ctx context.Context, req ServerProductRequest, out *ServerProductResolution, installer string) error {
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
	if profile.Minecraft != req.Minecraft.String() {
		return fmt.Errorf("%s installer targets Minecraft %s, requested %s", req.Product, profile.Minecraft, req.Minecraft)
	}
	out.ProductVersion = req.ProductVersion
	if floating(out.ProductVersion) {
		out.ProductVersion = versionpkg.Version(strings.TrimSuffix(strings.TrimPrefix(out.Artifact.Filename, req.Product.String()+"-"), "-installer.jar"))
		if req.Product == server.ProductForge {
			out.ProductVersion = versionpkg.Version(strings.TrimPrefix(out.ProductVersion.String(), req.Minecraft.String()+"-"))
		}
	}
	out.Artifact.Version = out.ProductVersion
	out.Bootstrapper = "forge-installer"
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
		input, downloadable, e := libraryInput(lib, &z.Reader, out.ProductVersion)
		if e != nil {
			return e
		}
		if !downloadable {
			log.Debug(
				"routing: installer library skipped",
				"library", lib.Name,
				"reason", "no downloadable artifact",
			)
			continue
		}
		if seen[input.Path] {
			log.Debug(
				"routing: installer library skipped",
				"library", lib.Name,
				"path", input.Path,
				"reason", "duplicate library path",
			)
			continue
		}
		seen[input.Path] = true
		out.Inputs = append(out.Inputs, input)
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
		if v.ID == req.Minecraft.String() {
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
				out.Inputs = append(out.Inputs, server.BootstrapInput{Path: "server-mappings.txt", Provider: catalog.ProviderMojang, ProjectID: "server-mappings", Version: req.Minecraft, Filename: "server-mappings.txt", URL: mapping.URL, Hashes: map[string]string{"sha1": mapping.SHA1}})
			}
			break
		}
	}
	log.Debug(
		"routing: installer inputs resolved",
		"product", req.Product,
		"version", out.ProductVersion,
		"inputs", len(out.Inputs),
	)
	return nil
}
