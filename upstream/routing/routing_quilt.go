package routing

import (
	"context"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
)

// resolveQuilt resolves a Quilt loader server. The loader JAR is the core
// artifact; the profile's libraries are pinned beside the vanilla server JAR.
// Bootstrap generates the launcher JAR and its properties, so neither is an
// input. Quilt publishes no stable flag for loader versions, so the selector
// is resolved from the version suffix.
func (r *Router) resolveQuilt(
	ctx context.Context,
	req upstream.ServerProductRequest,
	out *upstream.ServerProductResolution,
) error {
	selected, err := r.quilt.ResolveVersionSelector(
		ctx,
		catalog.VersionedRef{
			Ref:       catalog.Ref{Name: "quilt"},
			Ecosystem: catalog.EcosystemQuilt,
			Version:   req.ProductVersion,
		},
	)
	if err != nil {
		return err
	}
	out.ProductVersion = selected.Version

	profile, err := r.quilt.ServerProfile(
		ctx,
		req.Minecraft,
		out.ProductVersion,
	)
	if err != nil {
		return err
	}

	artifact, err := r.quilt.LoaderArtifact(ctx, out.ProductVersion)
	if err != nil {
		return err
	}
	if err := r.trustMavenChecksum(ctx, &artifact); err != nil {
		return err
	}
	out.Artifact = artifact
	out.Bootstrapper = server.BootstrapMethodQuilt

	inputs, err := r.quilt.PinnedInputs(
		ctx,
		profile,
		out.ProductVersion,
	)
	if err != nil {
		return err
	}
	out.Inputs = append(out.Inputs, inputs...)

	r.logger.Debug(
		"routing: server product route selected",
		"product", req.Product,
		"provider", catalog.ProviderQuilt,
		"version", out.ProductVersion,
		"libraries", len(inputs),
	)
	return nil
}
