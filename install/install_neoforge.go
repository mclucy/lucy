package install

import "github.com/mclucy/lucy/types"

func init() {
	registerInstaller(types.Neoforge, installNeoForgeMod)
}

func installNeoForgeMod(p types.Package) error {
	return installModLoaderPackage(p, types.Neoforge)
}

func installNeoForge(id types.PackageId) error {
	panic("NeoForge installation is not implemented yet")
}
