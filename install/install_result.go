package install

import "github.com/mclucy/lucy/types"

type Result struct {
	Installed  []types.InstalledPackage
	Provenance map[string][]string
}
