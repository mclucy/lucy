package install

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

func Install(req types.PackageRequest, options InstallOptions) (*Result, error) {
	id := types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: req.Platform,
			Name:     req.Name,
		},
		Version: req.Version,
	}

	// for regular (non-identity) packages, delegate to InstallMany to unify
	// resolver behavior with batch adds
	if !types.IsIdentityPackage(id.PackageRef) {
		return InstallMany([]types.PackageRequest{req}, options)
	}

	// identity packages go through the established platform installer
	if id.Version == types.VersionAny {
		id.Version = types.VersionCompatible
	}

	if err := installPlatform(id); err != nil {
		return nil, err
	}

	return &Result{}, nil
}

func installPlatform(id types.VersionedPackageRef) error {
	serverInfo := workspace.ServerInfo()
	serverDir := serverInfo.Root

	bootstrapper, err := bootstrap.For(id.Platform)
	if err != nil {
		return err
	}

	if id.Platform == types.PlatformMCDR {
		return bootstrapper.Bootstrap(context.Background(), types.ResolvedPackage{}, serverDir)
	}

	installer, ok := routing.PlatformInstallerFor(id.Platform)
	if !ok {
		return fmt.Errorf("cannot install platform: %s", id.Platform)
	}

	resolved, err := installer.ResolveVersionSelector(id)
	if err != nil {
		return fmt.Errorf("resolve version failed: %w", err)
	}

	fetched, err := installer.Fetch(resolved)
	if err != nil {
		return fmt.Errorf("fetch platform artifact failed: %w", err)
	}

	return bootstrapper.Bootstrap(context.Background(), fetched, serverDir)
}
