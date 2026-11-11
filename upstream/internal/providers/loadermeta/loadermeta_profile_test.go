package loadermeta

import (
	"maps"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/version"
)

// quiltDefinition mirrors the loader definition routing binds for Quilt.
func quiltDefinition() Definition {
	return Definition{
		APIBase:    "https://meta.quiltmc.org",
		APIVersion: "v3",
		MavenBase:  "https://maven.quiltmc.org/repository/release/",
		Coordinate: "org.quiltmc:quilt-loader",
		Provider:   catalog.ProviderQuilt,
		ProjectID:  "quilt-loader",
	}
}

// quiltLoaderFixture mirrors one entry of the live /v3/versions/loader
// response.
func quiltLoaderFixture() LoaderVersion {
	return LoaderVersion{
		Version: version.Version("0.27.1"),
		Maven:   "org.quiltmc:quilt-loader:0.27.1",
		Hashes: map[string]string{
			"sha1":   "bf9be87ff9ed50acb787a821c2ff1ddb3da2522b",
			"sha256": "bdb27521ee0522c050dc189b9e27e537b59088165ee340c85d2c745b918ca03b",
		},
	}
}

func TestLoaderArtifactPinsQuiltMavenURLAndHashes(t *testing.T) {
	entry := quiltLoaderFixture()
	provider := New(nil, quiltDefinition())

	artifact, err := provider.LoaderArtifact(entry)
	if err != nil {
		t.Fatalf("build loader artifact: %v", err)
	}
	if artifact.Provider != catalog.ProviderQuilt {
		t.Errorf(
			"loader provider = %q, want %q",
			artifact.Provider,
			catalog.ProviderQuilt,
		)
	}
	if artifact.ProjectID != "quilt-loader" {
		t.Errorf("loader project = %q, want quilt-loader", artifact.ProjectID)
	}
	if artifact.Version != version.Version("0.27.1") {
		t.Errorf("loader version = %q, want 0.27.1", artifact.Version)
	}
	if artifact.Filename != "quilt-loader-0.27.1.jar" {
		t.Errorf(
			"loader filename = %q, want quilt-loader-0.27.1.jar",
			artifact.Filename,
		)
	}
	const wantURL = "https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-loader/0.27.1/quilt-loader-0.27.1.jar"
	if artifact.URL != wantURL {
		t.Errorf("loader URL = %q, want %q", artifact.URL, wantURL)
	}
	if !maps.Equal(artifact.Hashes, entry.Hashes) {
		t.Errorf(
			"loader hashes = %v, want %v",
			artifact.Hashes,
			entry.Hashes,
		)
	}

	// The artifact must own its hash map: a later metadata mutation cannot
	// rewrite the pinned checksums.
	artifact.Hashes["sha1"] = "tampered"
	if entry.Hashes["sha1"] != quiltLoaderFixture().Hashes["sha1"] {
		t.Fatalf("loader artifact aliases the metadata hash map")
	}
}

func TestLoaderArtifactUsesDefinitionCoordinate(t *testing.T) {
	provider := New(nil, Definition{
		MavenBase:  "https://maven.fabricmc.net/",
		Coordinate: "net.fabricmc:fabric-loader",
		Provider:   catalog.ProviderFabric,
		ProjectID:  "fabric-loader",
	})
	artifact, err := provider.LoaderArtifact(LoaderVersion{
		Version: version.Version("0.16.9"),
	})
	if err != nil {
		t.Fatalf("build fabric loader artifact: %v", err)
	}
	const wantURL = "https://maven.fabricmc.net/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar"
	if artifact.URL != wantURL {
		t.Errorf("loader URL = %q, want %q", artifact.URL, wantURL)
	}
	if artifact.Filename != "fabric-loader-0.16.9.jar" {
		t.Errorf("loader filename = %q", artifact.Filename)
	}
	if artifact.Provider != catalog.ProviderFabric {
		t.Errorf("loader provider = %q", artifact.Provider)
	}
}

func TestLoaderArtifactRejectsInvalidMavenCoordinate(t *testing.T) {
	provider := New(nil, quiltDefinition())
	if artifact, err := provider.LoaderArtifact(LoaderVersion{
		Version: version.Version("0.27.1"),
		Maven:   "not-a-coordinate",
	}); err == nil {
		t.Fatalf("invalid loader maven = %+v, want an error", artifact)
	}
}
