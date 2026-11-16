package routing

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream/internal/providers/loadermeta"
	"github.com/mclucy/lucy/version"
)

// quiltLoaderMetaProvider mirrors the provider routing binds for Quilt.
func quiltLoaderMetaProvider() *loadermeta.Provider {
	return loadermeta.New(nil, loadermeta.Definition{
		APIBase:    "https://meta.quiltmc.org",
		APIVersion: "v3",
		MavenBase:  "https://maven.quiltmc.org/repository/release/",
		Coordinate: "org.quiltmc:quilt-loader",
		Provider:   catalog.ProviderQuilt,
		ProjectID:  "quilt-loader",
	})
}

// quiltProfileFixture mirrors the Quilt Meta v3 server profile for
// quilt-loader 0.27.1 on Minecraft 1.21.1. The libraries intentionally span
// two Maven hosts and include the loader itself, matching the live response.
func quiltProfileFixture() loadermeta.Profile {
	return loadermeta.Profile{
		Libraries: []loadermeta.Library{
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

func inputPaths(inputs []server.BootstrapInput) []string {
	paths := make([]string, 0, len(inputs))
	for _, input := range inputs {
		paths = append(paths, input.Path)
	}
	return paths
}

func TestLoaderProfileInputsPinsEveryNonLoaderLibrary(t *testing.T) {
	inputs, err := loaderProfileInputs(
		quiltLoaderMetaProvider(),
		quiltProfileFixture(),
		version.Version("0.27.1"),
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
	for _, input := range inputs {
		if input.URL != wantURLs[input.Path] {
			t.Errorf(
				"%s URL = %q, want %q",
				input.Path,
				input.URL,
				wantURLs[input.Path],
			)
		}
		if input.Provider != catalog.ProviderMaven {
			t.Errorf(
				"%s provider = %q, want %q",
				input.Path,
				input.Provider,
				catalog.ProviderMaven,
			)
		}
		if input.Version != version.Version("0.27.1") {
			t.Errorf(
				"%s version = %q, want the selected loader version",
				input.Path,
				input.Version,
			)
		}
	}
}

func TestLoaderProfileInputsExcludesLoaderFromInputs(t *testing.T) {
	profile := loadermeta.Profile{
		Libraries: []loadermeta.Library{
			{
				Name: "org.quiltmc:quilt-loader:0.27.1",
				URL:  "https://maven.quiltmc.org/repository/release/",
			},
		},
	}

	inputs, err := loaderProfileInputs(
		quiltLoaderMetaProvider(),
		profile,
		version.Version("0.27.1"),
	)
	if err != nil {
		t.Fatalf("translate loader-only profile: %v", err)
	}
	if len(inputs) != 0 {
		t.Fatalf(
			"loader-only profile inputs = %v, want none",
			inputPaths(inputs),
		)
	}
}

func TestLoaderProfileInputsRejectsInvalidCoordinate(t *testing.T) {
	profile := loadermeta.Profile{
		Libraries: []loadermeta.Library{
			{
				Name: "not-a-coordinate",
				URL:  "https://maven.quiltmc.org/repository/release/",
			},
		},
	}

	if inputs, err := loaderProfileInputs(
		quiltLoaderMetaProvider(),
		profile,
		version.Version("0.27.1"),
	); err == nil {
		t.Fatalf(
			"invalid profile coordinate produced %d inputs, want an error",
			len(inputs),
		)
	}
}

func TestLoaderProfileInputsEmptyProfile(t *testing.T) {
	inputs, err := loaderProfileInputs(
		quiltLoaderMetaProvider(),
		loadermeta.Profile{},
		version.Version("0.27.1"),
	)
	if err != nil {
		t.Fatalf("translate empty profile: %v", err)
	}
	if len(inputs) != 0 {
		t.Fatalf("empty profile inputs = %v, want none", inputPaths(inputs))
	}
}
