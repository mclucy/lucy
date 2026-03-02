package install

import (
	"github.com/mclucy/lucy/types"
)

func init() {
	registerInstaller(types.Mcdr, installMcdrPlugin)
}

func installMcdrPlugin(p types.Package) error {
	panic("not implemented yet")
}
