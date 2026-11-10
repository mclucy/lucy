package quilt

import (
	"maps"
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

// quiltProfileFixture mirrors the Quilt Meta v3 server profile for
// quilt-loader 0.27.1 on Minecraft 1.21.1. The libraries intentionally span
// two Maven hosts and include the loader itself, matching the live response.
func quiltProfileFixture() ServerProfile {
	return ServerProfile{
		ID:                "quilt-loader-0.27.1-1.21.1",
		InheritsFrom:      "1.21.1",
		MainClass:         "org.quiltmc.loader.impl.launch.knot.KnotServer",
		LauncherMainClass: "org.quiltmc.loader.impl.launch.server.QuiltServerLauncher",
		Libraries: []Library{
			{
				Name: "net.fabricmc:sponge-mixin:0.15.4+mixin.0.8.7",
				URL:  "https://maven.fabricmc.net/",
			},
			{
				Name: "org.quiltmc:quilt-json5:1.0.4+final",
				URL:  "https://maven.quiltmc.org/repository/release/",
			},
			{
				Name: "org.quiltmc:quilt-loader:0.27.1",
				URL:  "https://maven.quiltmc.org/repository/release/",
			},
			{
				Name: "net.fabricmc:intermediary:1.21.1",
				URL:  "https://maven.fabricmc.net/",
			},
		},
	}
}

// quiltLoaderFixture mirrors one entry of the live /v3/versions/loader
// response. The version list carries the loader hashes, while the server
// profile libraries carry none.
func quiltLoaderFixture() LoaderVersion {
	return LoaderVersion{
		Maven:     "org.quiltmc:quilt-loader:0.27.1",
		Version:   "0.27.1",
		Build:     1,
		Separator: ".",
		FileSize:  2489817,
		Hashes: map[string]string{
			"sha1":   "bf9be87ff9ed50acb787a821c2ff1ddb3da2522b",
			"sha256": "bdb27521ee0522c050dc189b9e27e537b59088165ee340c85d2c745b918ca03b",
			"sha512": "568e3d4bdf9dfc5a309bd4c6d78bcd056e31dca2c5c62ae8adb36bc20d2cb6b7d305ffd7a23d4854b5622459f0a52ead2d61f69a7c0d8ecba967c1af44752d97",
		},
	}
}

// quiltLibraryHashes mirrors the Maven sidecar digests the provider fetches for
// each non-loader profile library.
func quiltLibraryHashes() map[string]map[string]string {
	return map[string]map[string]string{
		"net.fabricmc:sponge-mixin:0.15.4+mixin.0.8.7": {
			"sha256": "4631f67da980ae2ac9e6e285a7df4680645e2f37a6a14acc775f97982162c2a2",
		},
		"org.quiltmc:quilt-json5:1.0.4+final": {
			"sha256": "c07d781820af7dc304de7c7fda9c4dd9c479851d451b1cb473851d6af226ac37",
		},
		"net.fabricmc:intermediary:1.21.1": {
			"sha256": "6059157dfb4a536ec151697004f74f6004b7d346b289c2f89849e0e585aa36fa",
		},
	}
}

func inputPaths(inputs []server.BootstrapInput) []string {
	paths := make([]string, 0, len(inputs))
	for _, input := range inputs {
		paths = append(paths, input.Path)
	}
	return paths
}

func TestSelectLoaderVersionStableRejectsPrereleases(t *testing.T) {
	// The live list is not grouped by channel: prerelease builds sit above and
	// between stable releases. Stable selection must skip every prerelease.
	versions := []LoaderVersion{
		{Version: "0.31.0-beta.6"},
		{Version: "0.30.2"},
		{Version: "0.31.0-beta.5"},
		{Version: "0.30.2-beta.1"},
		{Version: "0.30.1"},
		{Version: "0.30.1-beta.4"},
		{Version: "0.29.1"},
	}

	got, err := selectLoaderVersion(versions, version.Stable)
	if err != nil {
		t.Fatalf("select stable loader version: %v", err)
	}
	if got.Version != "0.30.2" {
		t.Fatalf("stable loader = %q, want 0.30.2", got.Version)
	}
}

func TestSelectLoaderVersionStableWithoutStableRelease(t *testing.T) {
	versions := []LoaderVersion{
		{Version: "0.31.0-beta.6"},
		{Version: "0.31.0-beta.5"},
		{Version: "0.30.2-beta.1"},
	}

	if got, err := selectLoaderVersion(versions, version.Stable); err == nil {
		t.Fatalf("stable loader = %q, want an error when no stable release exists", got.Version)
	}
}

func TestSelectLoaderVersionExplicit(t *testing.T) {
	versions := []LoaderVersion{
		{Version: "0.31.0-beta.6"},
		{Version: "0.30.2"},
		{Version: "0.30.1-beta.4"},
		{Version: "0.29.1"},
	}

	for _, want := range []string{"0.31.0-beta.6", "0.30.2", "0.30.1-beta.4", "0.29.1"} {
		t.Run(want, func(t *testing.T) {
			got, err := selectLoaderVersion(versions, version.Version(want))
			if err != nil {
				t.Fatalf("select explicit loader %q: %v", want, err)
			}
			if got.Version != want {
				t.Fatalf("explicit loader = %q, want %q", got.Version, want)
			}
		})
	}
}

func TestSelectLoaderVersionExplicitUnknown(t *testing.T) {
	versions := []LoaderVersion{
		{Version: "0.30.2"},
		{Version: "0.29.1"},
	}

	if got, err := selectLoaderVersion(versions, version.Version("0.30.0")); err == nil {
		t.Fatalf("explicit loader = %q, want an error for an unpublished version", got.Version)
	}
}

func TestSelectLoaderVersionFloatingUsesNewestEntry(t *testing.T) {
	versions := []LoaderVersion{
		{Version: "0.31.0-beta.6"},
		{Version: "0.30.2"},
		{Version: "0.29.1"},
	}

	for _, selector := range []version.Version{version.Any, version.Beta} {
		t.Run(selector.String(), func(t *testing.T) {
			got, err := selectLoaderVersion(versions, selector)
			if err != nil {
				t.Fatalf("select %s loader version: %v", selector, err)
			}
			if got.Version != "0.31.0-beta.6" {
				t.Fatalf("%s loader = %q, want newest entry 0.31.0-beta.6", selector, got.Version)
			}
		})
	}
}

func TestSelectLoaderVersionRejectsEmptyListAndInvalidSelector(t *testing.T) {
	if got, err := selectLoaderVersion(nil, version.Stable); err == nil {
		t.Fatalf("empty loader metadata = %q, want an error", got.Version)
	}
	for _, selector := range []version.Version{version.Unknown, version.None, ""} {
		if got, err := selectLoaderVersion(
			[]LoaderVersion{{Version: "0.30.2"}},
			selector,
		); err == nil {
			t.Fatalf("selector %q = %q, want an error", selector, got.Version)
		}
	}
}

func TestSelectGameVersion(t *testing.T) {
	versions := []GameVersion{
		{Version: "1.21.2-pre1", Stable: false},
		{Version: "1.21.1", Stable: true},
		{Version: "1.21", Stable: true},
		{Version: "1.20.6", Stable: true},
	}

	stable, err := selectGameVersion(versions, version.Stable)
	if err != nil {
		t.Fatalf("select stable game version: %v", err)
	}
	if stable.Version != "1.21.1" {
		t.Fatalf("stable game = %q, want 1.21.1", stable.Version)
	}

	for _, selector := range []version.Version{version.Any, version.Beta} {
		got, err := selectGameVersion(versions, selector)
		if err != nil {
			t.Fatalf("select %s game version: %v", selector, err)
		}
		if got.Version != "1.21.2-pre1" {
			t.Fatalf("%s game = %q, want newest entry", selector, got.Version)
		}
	}

	explicit, err := selectGameVersion(versions, version.Version("1.20.6"))
	if err != nil {
		t.Fatalf("select explicit game version: %v", err)
	}
	if explicit.Version != "1.20.6" {
		t.Fatalf("explicit game = %q, want 1.20.6", explicit.Version)
	}

	if got, err := selectGameVersion(versions, version.Version("1.21.3")); err == nil {
		t.Fatalf("unknown game = %q, want an error", got.Version)
	}
}

func TestLoaderArtifactPinsQuiltMavenURLAndHashes(t *testing.T) {
	entry := quiltLoaderFixture()

	artifact, err := loaderArtifact(entry)
	if err != nil {
		t.Fatalf("build loader artifact: %v", err)
	}
	if artifact.Provider != catalog.ProviderQuilt {
		t.Errorf("loader provider = %q, want %q", artifact.Provider, catalog.ProviderQuilt)
	}
	if artifact.ProjectID != "quilt-loader" {
		t.Errorf("loader project = %q, want quilt-loader", artifact.ProjectID)
	}
	if artifact.Version != version.Version("0.27.1") {
		t.Errorf("loader version = %q, want 0.27.1", artifact.Version)
	}
	if artifact.Filename != "quilt-loader-0.27.1.jar" {
		t.Errorf("loader filename = %q, want quilt-loader-0.27.1.jar", artifact.Filename)
	}
	const wantURL = "https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-loader/0.27.1/quilt-loader-0.27.1.jar"
	if artifact.URL != wantURL {
		t.Errorf("loader URL = %q, want %q", artifact.URL, wantURL)
	}
	if !maps.Equal(artifact.Hashes, entry.Hashes) {
		t.Errorf("loader hashes = %v, want %v", artifact.Hashes, entry.Hashes)
	}

	// The artifact must own its hash map: a later metadata mutation cannot
	// rewrite the pinned checksums.
	artifact.Hashes["sha1"] = "tampered"
	if entry.Hashes["sha1"] != quiltLoaderFixture().Hashes["sha1"] {
		t.Fatalf("loader artifact aliases the metadata hash map")
	}
}

func TestLoaderArtifactRejectsInvalidMavenCoordinate(t *testing.T) {
	if artifact, err := loaderArtifact(LoaderVersion{Maven: "not-a-coordinate", Version: "0.27.1"}); err == nil {
		t.Fatalf("invalid loader maven = %+v, want an error", artifact)
	}
}

func TestLibraryArtifactUsesLibraryHost(t *testing.T) {
	lib := Library{
		Name: "org.quiltmc:quilt-json5:1.0.4+final",
		URL:  "https://maven.quiltmc.org/repository/release/",
	}

	artifact, err := libraryArtifact(lib)
	if err != nil {
		t.Fatalf("build library artifact: %v", err)
	}
	if artifact.Provider != catalog.ProviderMaven {
		t.Errorf("library provider = %q, want %q", artifact.Provider, catalog.ProviderMaven)
	}
	if artifact.ProjectID != lib.Name {
		t.Errorf("library project = %q, want %q", artifact.ProjectID, lib.Name)
	}
	if artifact.Filename != "quilt-json5-1.0.4+final.jar" {
		t.Errorf("library filename = %q, want quilt-json5-1.0.4+final.jar", artifact.Filename)
	}
	const wantURL = "https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-json5/1.0.4+final/quilt-json5-1.0.4+final.jar"
	if artifact.URL != wantURL {
		t.Errorf("library URL = %q, want %q", artifact.URL, wantURL)
	}
	if artifact.Hashes == nil || len(artifact.Hashes) != 0 {
		t.Errorf("library hashes = %v, want an empty map", artifact.Hashes)
	}
	if artifact.Version != "" {
		t.Errorf("library version = %q, want it filled by profileInputs", artifact.Version)
	}
}

func TestLibraryArtifactTrimsHostTrailingSlash(t *testing.T) {
	for _, host := range []string{
		"https://maven.fabricmc.net",
		"https://maven.fabricmc.net/",
	} {
		artifact, err := libraryArtifact(Library{
			Name: "net.fabricmc:intermediary:1.21.1",
			URL:  host,
		})
		if err != nil {
			t.Fatalf("build library artifact for %q: %v", host, err)
		}
		const wantURL = "https://maven.fabricmc.net/net/fabricmc/intermediary/1.21.1/intermediary-1.21.1.jar"
		if artifact.URL != wantURL {
			t.Errorf("host %q URL = %q, want %q", host, artifact.URL, wantURL)
		}
	}
}

func TestLibraryArtifactRejectsMissingHostAndCoordinate(t *testing.T) {
	if artifact, err := libraryArtifact(Library{Name: "org.quiltmc:quilt-json5:1.0.4+final"}); err == nil {
		t.Fatalf("empty library host = %+v, want an error", artifact)
	}
	if artifact, err := libraryArtifact(Library{Name: "not-a-coordinate", URL: "https://maven.quiltmc.org/repository/release/"}); err == nil {
		t.Fatalf("invalid library coordinate = %+v, want an error", artifact)
	}
}

func TestProfileInputsPinsEveryNonLoaderLibrary(t *testing.T) {
	inputs, err := profileInputs(
		quiltProfileFixture(),
		version.Version("0.27.1"),
		quiltLibraryHashes(),
	)
	if err != nil {
		t.Fatalf("translate server profile: %v", err)
	}

	// The loader is the core artifact; Fabric and Quilt profiles both list it
	// among the libraries, but it must not be pinned as a bootstrap input.
	wantPaths := []string{
		"libraries/net/fabricmc/sponge-mixin/0.15.4+mixin.0.8.7/sponge-mixin-0.15.4+mixin.0.8.7.jar",
		"libraries/org/quiltmc/quilt-json5/1.0.4+final/quilt-json5-1.0.4+final.jar",
		"libraries/net/fabricmc/intermediary/1.21.1/intermediary-1.21.1.jar",
	}
	if got := inputPaths(inputs); !slices.Equal(got, wantPaths) {
		t.Fatalf("profile input order = %v, want %v", got, wantPaths)
	}

	wantURLs := map[string]string{
		wantPaths[0]: "https://maven.fabricmc.net/net/fabricmc/sponge-mixin/0.15.4+mixin.0.8.7/sponge-mixin-0.15.4+mixin.0.8.7.jar",
		wantPaths[1]: "https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-json5/1.0.4+final/quilt-json5-1.0.4+final.jar",
		wantPaths[2]: "https://maven.fabricmc.net/net/fabricmc/intermediary/1.21.1/intermediary-1.21.1.jar",
	}
	hashes := quiltLibraryHashes()
	for _, input := range inputs {
		if input.URL != wantURLs[input.Path] {
			t.Errorf("%s URL = %q, want %q", input.Path, input.URL, wantURLs[input.Path])
		}
		if input.Provider != catalog.ProviderMaven {
			t.Errorf("%s provider = %q, want %q", input.Path, input.Provider, catalog.ProviderMaven)
		}
		if input.Version != version.Version("0.27.1") {
			t.Errorf("%s version = %q, want the selected loader version", input.Path, input.Version)
		}
		if !maps.Equal(input.Hashes, hashes[input.ProjectID]) {
			t.Errorf("%s hashes = %v, want %v", input.Path, input.Hashes, hashes[input.ProjectID])
		}
	}
}

func TestProfileInputsExcludesLoaderFromInputs(t *testing.T) {
	profile := ServerProfile{
		Libraries: []Library{
			{
				Name: "org.quiltmc:quilt-loader:0.27.1",
				URL:  "https://maven.quiltmc.org/repository/release/",
			},
		},
	}

	inputs, err := profileInputs(profile, version.Version("0.27.1"), nil)
	if err != nil {
		t.Fatalf("translate loader-only profile: %v", err)
	}
	if len(inputs) != 0 {
		t.Fatalf("loader-only profile inputs = %v, want none", inputPaths(inputs))
	}
}

func TestProfileInputsRequiresPinnedLibraryHashes(t *testing.T) {
	hashes := quiltLibraryHashes()
	delete(hashes, "net.fabricmc:intermediary:1.21.1")

	inputs, err := profileInputs(
		quiltProfileFixture(),
		version.Version("0.27.1"),
		hashes,
	)
	if err == nil {
		t.Fatalf("missing library hash produced %d inputs, want an error", len(inputs))
	}
}

func TestProfileInputsRejectsInvalidCoordinate(t *testing.T) {
	profile := ServerProfile{
		Libraries: []Library{
			{Name: "not-a-coordinate", URL: "https://maven.quiltmc.org/repository/release/"},
		},
	}

	if inputs, err := profileInputs(
		profile,
		version.Version("0.27.1"),
		map[string]map[string]string{"not-a-coordinate": {"sha256": "00"}},
	); err == nil {
		t.Fatalf("invalid profile coordinate produced %d inputs, want an error", len(inputs))
	}
}

func TestProfileInputsEmptyProfile(t *testing.T) {
	inputs, err := profileInputs(ServerProfile{}, version.Version("0.27.1"), nil)
	if err != nil {
		t.Fatalf("translate empty profile: %v", err)
	}
	if len(inputs) != 0 {
		t.Fatalf("empty profile inputs = %v, want none", inputPaths(inputs))
	}
}
