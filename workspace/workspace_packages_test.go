package workspace

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace/discovery"
)

func writeObservationJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir jar directory: %v", err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create jar %s: %v", path, err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create jar entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write jar entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close jar writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close jar file: %v", err)
	}
}

func writeObservationFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func fabricObservationIdentity(minecraft, loader string) discovery.Identity {
	return discovery.Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemFabric,
			Name:      catalog.Name("fabric"),
			Version:   versionpkg.Version(loader),
		},
		Components: []catalog.VersionedRef{
			{
				Ecosystem: catalog.EcosystemFabric,
				Name:      catalog.Name("fabric-loader"),
				Version:   versionpkg.Version(loader),
			},
			{
				Ecosystem: catalog.EcosystemMinecraft,
				Name:      catalog.Name("minecraft"),
				Version:   versionpkg.Version(minecraft),
			},
		},
	}
}

// Package observation follows the selected runtime's package directories and
// keeps unreadable descriptors for the separate identification step.
func TestObserveReportsPackagesOfSelectedRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeObservationJar(
		t,
		filepath.Join(root, "fabric-server-mc.1.21.1-loader.0.19.5-launcher.1.1.2.jar"),
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: net.fabricmc.installer.ServerLauncher\n\n",
			"install.properties":   "fabric-loader-version=0.19.5\ngame-version=1.21.1\n",
		},
	)
	modsDir := filepath.Join(root, "mods")
	modPath := filepath.Join(modsDir, "sodium.jar")
	writeObservationJar(t, modPath, map[string]string{
		"fabric.mod.json": `{"schemaVersion":1,"id":"sodium","version":"0.6.0","name":"Sodium"}`,
	})
	unreadablePath := filepath.Join(modsDir, "corrupt.jar")
	writeObservationFile(t, unreadablePath, []byte("not a zip archive"))
	writeObservationFile(t, filepath.Join(modsDir, "readme.txt"), []byte("ignored"))

	observed := Observe(root, discovery.Request{})
	selection := observed.Runtime()
	if selection == nil {
		t.Fatalf(
			"no runtime selected: %v",
			observed.SelectionError(),
		)
	}
	if got := selection.MinecraftVersion().String(); got != "1.21.1" {
		t.Fatalf("Minecraft version = %q, want 1.21.1", got)
	}
	if got := observed.PackageDirs(); !slices.Equal(got, []string{modsDir}) {
		t.Fatalf("package directories = %v, want [%s]", got, modsDir)
	}
	if len(observed.Packages) != 1 {
		t.Fatalf("packages = %v, want the one descriptor-backed package", observed.Packages)
	}
	pkg := observed.Packages[0]
	if pkg.ID.Ecosystem != catalog.EcosystemFabric ||
		pkg.ID.Name.String() != "sodium" ||
		pkg.ID.Version.String() != "0.6.0" {
		t.Fatalf("package identity = %+v, want fabric:sodium@0.6.0", pkg.ID)
	}
	if pkg.Path != modPath {
		t.Fatalf("package path = %q, want %q", pkg.Path, modPath)
	}
	if got := observed.UnidentifiedPackageFiles; !slices.Equal(got, []string{unreadablePath}) {
		t.Fatalf(
			"unidentified package files = %v, want [%s]",
			got,
			unreadablePath,
		)
	}
}

// The package directory list is part of the reported status: loader runtimes
// observe mods/, Bukkit-family runtimes observe plugins/, and an unidentified
// runtime observes neither.
func TestPackageSearchPathsFollowSelectedRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fabricPath := filepath.Join(root, "fabric-server-launch.jar")
	paperPath := filepath.Join(root, "paper.jar")
	vanillaPath := filepath.Join(root, "server.jar")
	tests := []struct {
		name      string
		selection *discovery.Selection
		want      []string
	}{
		{
			name: "fabric runtime observes mods",
			selection: &discovery.Selection{
				State:        discovery.SelectionSelected,
				Installation: &discovery.Installation{Identity: fabricObservationIdentity("1.21.1", "0.19.5")},
				EntryPoint:   &discovery.File{Path: fabricPath, Role: discovery.RoleEntryPoint},
			},
			want: []string{filepath.Join(root, "mods")},
		},
		{
			name: "paper runtime observes plugins",
			selection: &discovery.Selection{
				State: discovery.SelectionSelected,
				Installation: &discovery.Installation{Identity: discovery.Identity{
					Primary: catalog.VersionedRef{
						Ecosystem: catalog.EcosystemPaper,
						Name:      catalog.Name("paper"),
						Version:   versionpkg.Version("130"),
					},
				}},
				EntryPoint: &discovery.File{Path: paperPath, Role: discovery.RoleEntryPoint},
			},
			want: []string{filepath.Join(root, "plugins")},
		},
		{
			name: "vanilla runtime observes no content directory",
			selection: &discovery.Selection{
				State: discovery.SelectionSelected,
				Installation: &discovery.Installation{Identity: discovery.Identity{
					Primary: catalog.VersionedRef{
						Ecosystem: catalog.EcosystemMinecraft,
						Name:      catalog.Name("minecraft"),
						Version:   versionpkg.Version("1.21.1"),
					},
				}},
				EntryPoint: &discovery.File{Path: vanillaPath, Role: discovery.RoleEntryPoint},
			},
		},
		{
			name: "unselected runtime observes no content directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := packageSearchPaths(tt.selection, root)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("package search paths = %v, want %v", got, tt.want)
			}
		})
	}
}
