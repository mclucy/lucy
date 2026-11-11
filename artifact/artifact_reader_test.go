package artifact

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

const forgeModsToml = `modLoader = "javafml"
loaderVersion = "[1,)"
license = "MIT"

[[mods]]
modId = "example"
version = "1.0.0"
displayName = "Example"
description = "Example"
authors = "Example"

[[dependencies.example]]
modId = "minecraft"
mandatory = true
versionRange = "[1.21.1,)"
ordering = "NONE"
side = "BOTH"
`

const neoforgeModsToml = `modLoader = "javafml"
loaderVersion = "[1,)"
license = "MIT"

[[mods]]
modId = "example"
version = "1.0.0"
displayName = "Example"
description = "Example"
authors = "Example"

[[dependencies.example]]
modId = "neoforge"
mandatory = true
versionRange = "[21.1.0,)"
ordering = "NONE"
side = "BOTH"
`

func TestReadInfoReportsForgeFamilyDescriptors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		entries    map[string]string
		ecosystems []catalog.Ecosystem
	}{
		{
			name: "forge layout",
			entries: map[string]string{
				"META-INF/mods.toml": forgeModsToml,
			},
			ecosystems: []catalog.Ecosystem{
				catalog.EcosystemForge,
				catalog.EcosystemNeoForge,
			},
		},
		{
			name: "legacy neoforge layout",
			entries: map[string]string{
				"META-INF/mods.toml": neoforgeModsToml,
			},
			ecosystems: []catalog.Ecosystem{
				catalog.EcosystemForge,
				catalog.EcosystemNeoForge,
			},
		},
		{
			name: "modern neoforge layout",
			entries: map[string]string{
				"META-INF/mods.toml":          forgeModsToml,
				"META-INF/neoforge.mods.toml": neoforgeModsToml,
			},
			ecosystems: []catalog.Ecosystem{
				catalog.EcosystemForge,
				catalog.EcosystemNeoForge,
			},
		},
		{
			name: "modern neoforge only",
			entries: map[string]string{
				"META-INF/neoforge.mods.toml": neoforgeModsToml,
			},
			ecosystems: []catalog.Ecosystem{catalog.EcosystemNeoForge},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := writeArtifactJar(t, test.entries)
			infos, err := ReadInfo(path)
			if err != nil {
				t.Fatalf("read artifact: %v", err)
			}
			if len(infos) != len(test.ecosystems) {
				t.Fatalf(
					"artifact yielded %d package identities, want %d: %+v",
					len(infos), len(test.ecosystems), infos,
				)
			}
			for _, ecosystem := range test.ecosystems {
				if !slices.ContainsFunc(infos, func(info PackageInfo) bool {
					return info.Ref.Ecosystem == ecosystem
				}) {
					t.Errorf("ecosystem %q is missing from %+v", ecosystem, infos)
				}
			}
		})
	}
}

func writeArtifactJar(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.jar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture jar: %v", err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("seal fixture jar: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close fixture jar: %v", err)
	}
	return path
}
