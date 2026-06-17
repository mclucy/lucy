package install

import "github.com/mclucy/lucy/types"

func init() {
	registerInstaller(types.PlatformNeoforge, installNeoForgeMod)
}

func installNeoForgeMod(p types.Package) error {
	return installModLoaderPackage(p, types.PlatformNeoforge)
}
