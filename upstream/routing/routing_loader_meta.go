package routing

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/internal/providers/loadermeta"
	"github.com/mclucy/lucy/version"
)

// loaderRuntime is the routing definition of one Fabric-style loader: its
// meta client definition, ecosystem, and bootstrap method.
type loaderRuntime struct {
	meta         loadermeta.Definition
	ecosystem    catalog.Ecosystem
	bootstrapper server.BootstrapMethod
}

// loaderRuntimes is keyed by the server product the definition resolves.
var loaderRuntimes = map[server.Product]loaderRuntime{
	server.ProductFabric: {
		meta: loadermeta.Definition{
			APIBase:    "https://meta.fabricmc.net",
			APIVersion: "v2",
			MavenBase:  "https://maven.fabricmc.net/",
			Coordinate: "net.fabricmc:fabric-loader",
			Provider:   catalog.ProviderFabric,
			ProjectID:  "fabric-loader",
			StableFlag: true,
		},
		ecosystem:    catalog.EcosystemFabric,
		bootstrapper: server.BootstrapMethodFabric,
	},
	server.ProductQuilt: {
		meta: loadermeta.Definition{
			APIBase:    "https://meta.quiltmc.org",
			APIVersion: "v3",
			MavenBase:  "https://maven.quiltmc.org/repository/release/",
			Coordinate: "org.quiltmc:quilt-loader",
			Provider:   catalog.ProviderQuilt,
			ProjectID:  "quilt-loader",
			StableFlag: false,
		},
		ecosystem:    catalog.EcosystemQuilt,
		bootstrapper: server.BootstrapMethodQuilt,
	},
}

func loaderRuntimeFor(product server.Product) (loaderRuntime, bool) {
	definition, ok := loaderRuntimes[product]
	return definition, ok
}

// loaderProfileInputs translates a profile into library bootstrap inputs.
// Profile order is preserved and the loader library is skipped. The caller
// applies the hash trust policy.
func loaderProfileInputs(
	provider *loadermeta.Provider,
	profile loadermeta.Profile,
	loader version.Version,
) ([]server.BootstrapInput, error) {
	inputs := make([]server.BootstrapInput, 0, len(profile.Libraries))
	for _, lib := range profile.Libraries {
		if provider.IsLoaderLibrary(lib.Name) {
			continue
		}
		input, downloadable, err := libraryInput(
			mavenLibrary{
				Name: lib.Name,
				URL:  lib.URL,
			},
			nil,
			loader,
		)
		if err != nil {
			return nil, err
		}
		if !downloadable {
			continue
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

// resolveLoaderProduct resolves a loader product through the shared meta
// flow: select the loader version, read its profile, pin the core artifact,
// and pin every profile library.
func (r *Router) resolveLoaderProduct(
	ctx context.Context,
	req upstream.ServerProductRequest,
	out *upstream.ServerProductResolution,
	definition loaderRuntime,
) error {
	provider, ok := r.loaders[req.Product]
	if !ok {
		return fmt.Errorf(
			"no loader meta provider for product %q", req.Product,
		)
	}
	selected, err := provider.ResolveLoaderVersion(ctx, req.ProductVersion)
	if err != nil {
		return err
	}
	out.ProductVersion = selected.Version

	profile, err := provider.ServerProfile(
		ctx,
		req.Minecraft,
		out.ProductVersion,
	)
	if err != nil {
		return err
	}

	artifact, err := provider.LoaderArtifact(selected)
	if err != nil {
		return err
	}
	if err := r.trustMavenChecksum(ctx, &artifact); err != nil {
		return err
	}
	out.Artifact = artifact
	out.Bootstrapper = definition.bootstrapper

	inputs, err := loaderProfileInputs(provider, profile, out.ProductVersion)
	if err != nil {
		return err
	}
	for i := range inputs {
		if err := r.trustMavenChecksum(ctx, &inputs[i].Artifact); err != nil {
			return err
		}
	}
	out.Inputs = append(out.Inputs, inputs...)

	r.logger.Debug(
		"routing: server product route selected",
		"product", req.Product,
		"provider", definition.meta.Provider,
		"version", out.ProductVersion,
		"libraries", len(inputs),
	)
	return nil
}
