package install

import "github.com/mclucy/lucy/types"

func init() {
	registerInstaller(types.Forge, installForgeMod)
}

func installForgeMod(p types.Package) error {
	return installModLoaderPackage(p, types.Forge)
}
