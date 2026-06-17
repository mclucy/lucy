package cmd

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/types"
)

func packageRequestFromInput(raw string, rawSource string) (install.PackageRequest, error) {
	id, err := input.Parse(strings.TrimSpace(raw))
	if err != nil {
		return install.PackageRequest{}, err
	}

	scope := types.ParseSource(strings.TrimSpace(rawSource))
	if scope == types.SourceUnknown {
		return install.PackageRequest{}, fmt.Errorf("unknown source %s", rawSource)
	}

	return install.PackageRequest{
		ScopedPackageRef: types.ScopedPackageRef{
			PackageRef: id.PackageRef,
			Scope:      scope,
		},
		Version: id.Version,
	}, nil
}
