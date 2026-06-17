package install

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

type platformInstaller func(p types.Package) error

func Install(req PackageRequest, options InstallOptions) (*Result, error) {
	// TODO(package-ref-migration): remove PackageId/source extraction once identity installers accept PackageRequest.
	id := types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: req.Platform,
			Name:     req.Name,
		},
		Version: req.Version,
	}
	source := req.Scope
	_ = source

	// for regular (non-identity) packages, delegate to InstallMany to unify
	// resolver behavior with batch adds
	if !types.IsIdentityPackage(id.PackageRef) {
		return InstallMany([]PackageRequest{req}, options)
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
	if id.Platform == types.PlatformMCDR {
		if serverInfo.Environments.Mcdr != nil {
			return errors.New("mcdr already installed")
		}
		return initMcdr()
	}

	installer, ok := routing.PlatformInstallerFor(id.Platform)
	if !ok {
		return fmt.Errorf("cannot install platform: %s", id.Platform)
	}
	return installer.InstallPlatform(id, serverInfo.Root)
}
