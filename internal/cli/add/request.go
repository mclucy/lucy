package add

import (
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
)

func packageRequestFromInput(raw string) (types.PackageRequest, error) {
	ref, err := input.ParseFullPackageRef(raw)
	if err != nil {
		return types.PackageRequest{}, err
	}

	return types.PackageRequest{FullPackageRef: ref}, nil
}
