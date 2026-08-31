package add

import (
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
)

func packageRequestFromInput(raw string) (types.PackageRequest, error) {
	return input.Parse(raw)
}
