package install

import "github.com/mclucy/lucy/types"

func init() {
	registerInstaller(types.PlatformFabric, installFabricMod)
}

func installFabricMod(p types.Package) error {
	return installModLoaderPackage(p, types.PlatformFabric)
}
