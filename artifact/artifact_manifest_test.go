package artifact

import (
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/version"
)

func TestManifestVersionAcrossArtifactViews(t *testing.T) {
	t.Parallel()
	loaders := []struct {
		name       string
		ecosystem  catalog.Ecosystem
		descriptor string
		metadata   string
	}{
		{
			name:       "forge",
			ecosystem:  catalog.EcosystemForge,
			descriptor: "META-INF/mods.toml",
			metadata:   forgeModsToml,
		},
		{
			name:       "neoforge",
			ecosystem:  catalog.EcosystemNeoForge,
			descriptor: "META-INF/neoforge.mods.toml",
			metadata:   neoforgeModsToml,
		},
	}
	manifests := []struct {
		name    string
		content string
		want    version.Version
	}{
		{
			name:    "lf at start",
			content: "Implementation-Version: 2.3.4\nManifest-Version: 1.0\n",
			want:    "2.3.4",
		},
		{
			name:    "crlf with earlier prefixed field",
			content: "Manifest-Version: 1.0\r\nX-Implementation-Version: 9.9.9\r\nImplementation-Version: 2.3.4\r\n\r\n",
			want:    "2.3.4",
		},
		{
			name:    "cr only",
			content: "Manifest-Version: 1.0\rImplementation-Version: 2.3.4\r\r",
			want:    "2.3.4",
		},
		{
			name:    "prefixed field is not a version",
			content: "Manifest-Version: 1.0\r\nX-Implementation-Version: 9.9.9\r\n\r\n",
			want:    version.Unknown,
		},
	}
	for _, loader := range loaders {
		t.Run(loader.name, func(t *testing.T) {
			t.Parallel()
			for _, manifest := range manifests {
				t.Run(manifest.name, func(t *testing.T) {
					t.Parallel()
					metadata := strings.Replace(
						loader.metadata,
						`version = "1.0.0"`,
						`version = "${file.jarVersion}"`,
						1,
					)
					path := writeArtifactJar(t, map[string]string{
						loader.descriptor:      metadata,
						"META-INF/MANIFEST.MF": manifest.content,
					})
					infos, err := ReadInfo(path)
					if err != nil {
						t.Fatalf("read artifact metadata: %v", err)
					}
					var observed PackageInfo
					found := false
					for _, info := range infos {
						if info.Ref.Ecosystem == loader.ecosystem {
							observed = info
							found = true
							break
						}
					}
					if !found || observed.Ref.Version != manifest.want {
						t.Fatalf("observed version: got %+v, want example@%s", infos, manifest.want)
					}
					modules, err := ReadModules(t.Context(), path, loader.ecosystem)
					if err != nil {
						t.Fatalf("read descriptor modules: %v", err)
					}
					if len(modules) != 1 || modules[0].ID != "example" || modules[0].Version != manifest.want {
						t.Fatalf("descriptor version: got %+v, want example@%s", modules, manifest.want)
					}
				})
			}
		})
	}
}
