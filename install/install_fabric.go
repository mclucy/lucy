package install

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

func init() {
	registerInstaller(types.Fabric, installFabricMod)
}

func installFabric(version string) error {
	return fmt.Errorf(
		"install fabric loader is not implemented yet: %s",
		version,
	)
}

func installFabricMod(p types.Package) error {
	if p.Id.Name == "fabric-loader" {
		err := installFabric(p.Id.Version.String())
		if err != nil {
			return err
		}
	}
	return installModLoaderPackage(p, types.Fabric)
}
