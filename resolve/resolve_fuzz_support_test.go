package resolve_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

const (
	fuzzMinecraft     = "1.21.1"
	fuzzLoaderVersion = "0.16.0"
	nestedJarPath     = "META-INF/jars/nested.jar"
)

const (
	depKindNone = iota
	depKindRequired
	depKindIncompatible
)

var (
	fuzzProjectNames = [3]string{"alpha", "beta", "gamma"}
	fuzzTarget       = resolve.Target{
		Product:     serverpkg.ProductFabric,
		Minecraft:   versionpkg.Version(fuzzMinecraft),
		CoreVersion: versionpkg.Version(fuzzLoaderVersion),
	}
	fuzzModuleTarget = resolve.Target{
		Product:     serverpkg.ProductFabric,
		Minecraft:   versionpkg.Version(fuzzMinecraft),
		CoreVersion: versionpkg.Version(fuzzLoaderVersion),
		CoreModules: []catalog.DescriptorModule{{
			ID:             "lucycore",
			Version:        versionpkg.Version("3.0.0"),
			Ecosystem:      catalog.EcosystemFabric,
			VersionDialect: versionpkg.DialectFabric,
		}},
	}
)

// stubHashes, stubFile, stubDependency, and stubVersion mirror only the
// Modrinth catalogue fields the resolver consumes. The stub catalogue answers
// the provider over HTTP so the public resolver path stays intact.
type stubHashes struct {
	SHA1   string `json:"sha1"`
	SHA512 string `json:"sha512"`
}

type stubFile struct {
	ID       string     `json:"id"`
	Hashes   stubHashes `json:"hashes"`
	URL      string     `json:"url"`
	Filename string     `json:"filename"`
	Primary  bool       `json:"primary"`
	FileType string     `json:"file_type"`
}

type stubDependency struct {
	ProjectID      string `json:"project_id"`
	DependencyType string `json:"dependency_type"`
}

type stubVersion struct {
	ID            string           `json:"id"`
	ProjectID     string           `json:"project_id"`
	VersionNumber string           `json:"version_number"`
	VersionType   string           `json:"version_type"`
	Status        string           `json:"status"`
	GameVersions  []string         `json:"game_versions"`
	Loaders       []string         `json:"loaders"`
	DatePublished time.Time        `json:"date_published"`
	Files         []stubFile       `json:"files"`
	Dependencies  []stubDependency `json:"dependencies"`
}

func selectionCandidateVersion(index int) string {
	if index == 0 {
		return "1.0.0"
	}
	return "2.0.0"
}

func selectionReleaseID(project string, index int) string {
	return fmt.Sprintf("%s-r%d", project, index)
}

func selectionFileID(project string, index int) string {
	return fmt.Sprintf("%s-f%d", project, index)
}

func selectionFilename(project string, index int) string {
	return fmt.Sprintf("%s-%d.jar", project, index)
}

func stubVersionEntry(
	project string,
	index int,
	version string,
	channel string,
	primary bool,
) stubVersion {
	fileID := selectionFileID(project, index)
	files := []stubFile{{
		ID:       fileID,
		URL:      "https://cdn.invalid/" + fileID + ".jar",
		Filename: selectionFilename(project, index),
		Primary:  primary,
	}}
	if !primary {
		// Modrinth infers the first file as primary when none is marked, so a
		// companion primary file with a non-empty type keeps the candidate
		// list unchanged while making the modeled file non-primary.
		files = append(files, stubFile{
			ID:       fileID + "-sources",
			URL:      "https://cdn.invalid/" + fileID + "-sources.jar",
			Filename: selectionFilename(project, index) + ".sources",
			Primary:  true,
			FileType: "sources",
		})
	}
	return stubVersion{
		ID:            selectionReleaseID(project, index),
		ProjectID:     project,
		VersionNumber: version,
		VersionType:   channel,
		Status:        "listed",
		GameVersions:  []string{fuzzMinecraft},
		Loaders:       []string{"fabric"},
		DatePublished: time.Unix(1_700_000_000, 0).UTC().Add(-time.Duration(index) * time.Hour),
		Files:         files,
		Dependencies:  []stubDependency{},
	}
}

type fixtureArchive struct {
	path   string
	hashes map[string]string
}

type catalogueFixture struct {
	dir       string
	artifacts map[string]fixtureArchive
	versions  map[string][]stubVersion
}

func newCatalogueFixture(t *testing.T) *catalogueFixture {
	t.Helper()
	return &catalogueFixture{
		dir:       t.TempDir(),
		artifacts: map[string]fixtureArchive{},
		versions:  map[string][]stubVersion{},
	}
}

func (f *catalogueFixture) addVersions(t *testing.T, project string, versions []stubVersion) {
	t.Helper()
	if _, exists := f.versions[project]; exists {
		t.Fatalf("duplicate fixture catalogue for %s", project)
	}
	f.versions[project] = slices.Clone(versions)
}

func (f *catalogueFixture) addArchive(t *testing.T, fileID string, data []byte) {
	t.Helper()
	if _, exists := f.artifacts[fileID]; exists {
		t.Fatalf("duplicate fixture archive for file %s", fileID)
	}
	path := filepath.Join(f.dir, fileID+".jar")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write fixture archive %s: %v", fileID, err)
	}
	sum := sha256.Sum256(data)
	f.artifacts[fileID] = fixtureArchive{
		path:   path,
		hashes: map[string]string{"sha256": hex.EncodeToString(sum[:])},
	}
}

func (f *catalogueFixture) materialize(a *catalog.Artifact) (artifact.MaterializedFile, error) {
	archive, ok := f.artifacts[a.FileID]
	if !ok {
		return artifact.MaterializedFile{}, fmt.Errorf("no fixture archive for artifact file %q", a.FileID)
	}
	return artifact.MaterializedFile{
		Path:   archive.path,
		Hashes: archive.hashes,
	}, nil
}

// candidates answers the resolver's injected catalogue capability directly
// from the fixture, mirroring the Modrinth fields resolution consumes.
func (f *catalogueFixture) candidates(
	_ context.Context,
	registry catalog.Registry,
	project, minecraft string,
	ecosystem catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	if registry != catalog.RegistryModrinth {
		return nil, fmt.Errorf("fixture has no %s catalogue", registry)
	}
	var out []upstream.Candidate
	for _, v := range f.versions[project] {
		if v.Status != "" && v.Status != "listed" {
			continue
		}
		if !slices.Contains(v.GameVersions, minecraft) ||
			!slices.Contains(v.Loaders, ecosystem.String()) {
			continue
		}
		unmarked := !slices.ContainsFunc(v.Files, func(file stubFile) bool { return file.Primary })
		for i, file := range v.Files {
			if file.FileType != "" {
				continue
			}
			hashes := map[string]string{}
			if file.Hashes.SHA512 != "" {
				hashes["sha512"] = file.Hashes.SHA512
			}
			if file.Hashes.SHA1 != "" {
				hashes["sha1"] = file.Hashes.SHA1
			}
			candidate := upstream.Candidate{
				Artifact: catalog.Artifact{
					Provider:  catalog.ProviderModrinth,
					ProjectID: v.ProjectID,
					ReleaseID: v.ID,
					FileID:    file.ID,
					Version:   versionpkg.Version(v.VersionNumber),
					Filename:  file.Filename,
					URL:       file.URL,
					Hashes:    hashes,
				},
				Channel:     upstream.ParseReleaseChannel(v.VersionType),
				PublishedAt: v.DatePublished,
				PrimaryFile: file.Primary || (unmarked && i == 0),
			}
			for _, dependency := range v.Dependencies {
				if dependency.ProjectID == "" {
					continue
				}
				candidate.Dependencies = append(candidate.Dependencies, upstream.ProjectDependency{
					Provider:  catalog.ProviderModrinth,
					ProjectID: dependency.ProjectID,
					Kind:      catalog.ParseDependencyKind(dependency.DependencyType),
				})
			}
			out = append(out, candidate)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("fixture project %s has no %s release for Minecraft %s", project, ecosystem, minecraft)
	}
	return out, nil
}

// fabricModuleSpec describes one descriptor module. Depends and Breaks name
// module identities; their constraints are empty so the fixture exercises
// presence and ownership rather than version-range matching.
type fabricModuleSpec struct {
	ID       string
	Version  string
	Provides []string
	Depends  []string
	Breaks   []string
}

func fabricArchive(t *testing.T, outer fabricModuleSpec, nested *fabricModuleSpec) []byte {
	t.Helper()
	var nestedBytes []byte
	if nested != nil {
		nestedBytes = fabricArchive(t, *nested, nil)
	}
	payload := map[string]any{
		"schemaVersion": 1,
		"id":            outer.ID,
		"version":       outer.Version,
		"environment":   "server",
	}
	if len(outer.Provides) > 0 {
		payload["provides"] = outer.Provides
	}
	if len(outer.Depends) > 0 {
		payload["depends"] = dependencyMap(outer.Depends)
	}
	if len(outer.Breaks) > 0 {
		payload["breaks"] = dependencyMap(outer.Breaks)
	}
	if nested != nil {
		payload["jars"] = []map[string]string{{"file": nestedJarPath}}
	}
	descriptor, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fixture Fabric descriptor: %v", err)
	}

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("fabric.mod.json")
	if err != nil {
		t.Fatalf("create fixture descriptor entry: %v", err)
	}
	if _, err := entry.Write(descriptor); err != nil {
		t.Fatalf("write fixture descriptor entry: %v", err)
	}
	if nested != nil {
		nestedEntry, err := writer.Create(nestedJarPath)
		if err != nil {
			t.Fatalf("create fixture nested JAR entry: %v", err)
		}
		if _, err := nestedEntry.Write(nestedBytes); err != nil {
			t.Fatalf("write fixture nested JAR entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close fixture archive: %v", err)
	}
	return buffer.Bytes()
}

func dependencyMap(ids []string) map[string]string {
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		out[id] = ""
	}
	return out
}
