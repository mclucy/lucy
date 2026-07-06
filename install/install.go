package install

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
)

func Install(
	ctx context.Context,
	req types.PackageRequest,
	options InstallOptions,
) (*Result, error) {
	options = options.withDefaults()
	id := types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Eco:  req.Eco,
			Name: req.Name,
		},
		Version: req.Version,
	}

	// for regular (non-identity) packages, delegate to InstallMany to unify
	// resolver behavior with batch adds
	if !types.IsIdentityPackage(id.PackageRef) {
		return InstallMany(ctx, []types.PackageRequest{req}, options)
	}

	// identity packages go through the established platform installer
	if err := installEcosystem(ctx, id, options); err != nil {
		return nil, err
	}

	return &Result{}, nil
}

func installEcosystem(
	ctx context.Context,
	id types.VersionedPackageRef,
	options InstallOptions,
) error {
	if err := ctx.Err(); err != nil {
		return installError(
			CategoryApply,
			err,
			map[string]any{"package": id.StringFull()},
		)
	}

	ws := options.Workspace()
	serverDir := ws.Root

	bootstrapper, err := bootstrap.ForEcosystem(id.Eco)
	if err != nil {
		return installError(
			CategoryResolution,
			err,
			map[string]any{"platform": id.Eco},
		)
	}

	if id.Eco == types.EcoMcdr {
		return installError(
			CategoryApply,
			bootstrapper.Bootstrap(ctx, types.ResolvedPackage{}, serverDir),
			map[string]any{"package": id.StringFull()},
		)
	}

	installer, ok := routing.EcosystemInstallerFor(id.Eco)
	if !ok {
		return installError(
			CategoryResolution,
			fmt.Errorf("cannot install platform: %s", id.Eco),
			map[string]any{"platform": id.Eco},
		)
	}

	resolved, err := installer.ResolveVersionSelector(id)
	if err != nil {
		return installError(
			CategoryResolution,
			fmt.Errorf("resolve version failed: %w", err),
			map[string]any{"package": id.StringFull()},
		)
	}

	fetched, err := installer.Fetch(resolved)
	if err != nil {
		return installError(
			CategoryDownload,
			fmt.Errorf("fetch platform artifact failed: %w", err),
			map[string]any{"package": id.StringFull()},
		)
	}

	return installError(
		CategoryApply,
		bootstrapper.Bootstrap(ctx, fetched, serverDir),
		map[string]any{"package": id.StringFull()},
	)
}
