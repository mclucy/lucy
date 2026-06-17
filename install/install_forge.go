package install

import "github.com/mclucy/lucy/types"

func init() {
	registerInstaller(types.PlatformForge, installForgeMod)
}

func installForgeMod(p types.Package) error {
	return installModLoaderPackage(p, types.PlatformForge)
}
