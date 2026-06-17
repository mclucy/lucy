package install

import "github.com/mclucy/lucy/types"

type Result struct {
	Installed  []types.Package
	Provenance map[string][]string
}
