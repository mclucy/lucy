package install

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

func Install(
	ctx context.Context,
	request types.PackageRequest,
	options InstallOptions,
) (*Result, error) {
	options = options.withDefaults()
	cores, regular := classifyInstallRequests(
		[]types.PackageRequest{request},
	)
	if len(cores) == 0 {
		return InstallMany(ctx, regular, options)
	}
	if err := prepareCoreRequests(cores); err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}
	if err := installCorePackage(ctx, cores[0], options); err != nil {
		return nil, err
	}
	workspace.Invalidate()
	return &Result{}, nil
}

func installCorePackage(
	ctx context.Context,
	request preparedCoreRequest,
	options InstallOptions,
) error {
	id := types.VersionedPackageRef{
		PackageRef: request.Match.Ref.PackageRef,
		Version:    request.Request.Version,
	}
	context := map[string]any{"package": id.StringFull()}
	if err := ctx.Err(); err != nil {
		return installError(CategoryApply, err, context)
	}

	ws := options.Workspace()
	serverDir := ws.Root
	bootstrapper, err := bootstrap.ForEcosystem(request.Binding.Ecosystem)
	if err != nil {
		return installError(CategoryResolution, err, context)
	}

	if request.Match.Core == types.CoreMCDReforged {
		return installError(
			CategoryApply,
			bootstrapper.Bootstrap(ctx, types.ResolvedPackage{}, serverDir),
			context,
		)
	}

	installer, ok := routing.DefaultRegistry().EcosystemInstaller(
		request.Binding.InstallerSource,
	)
	if !ok {
		return installError(
			CategoryResolution,
			fmt.Errorf(
				"no installer is registered for core package %s",
				request.Match.Core,
			),
			context,
		)
	}

	resolved, err := installer.ResolveVersionSelector(id)
	if err != nil {
		return installError(
			CategoryResolution,
			fmt.Errorf("resolve core package version: %w", err),
			context,
		)
	}

	fetched, err := installer.Fetch(resolved)
	if err != nil {
		return installError(
			CategoryDownload,
			fmt.Errorf("fetch core package artifact: %w", err),
			context,
		)
	}

	return installError(
		CategoryApply,
		bootstrapper.Bootstrap(ctx, fetched, serverDir),
		context,
	)
}
