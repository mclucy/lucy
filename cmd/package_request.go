package cmd

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/types"
)

func packageRequestFromInput(raw string, rawSource string) (install.PackageRequest, error) {
	ref, version, err := input.Parse(strings.TrimSpace(raw))
	if err != nil {
		return install.PackageRequest{}, err
	}

	if rawSource != "" {
		scope := types.ParseSource(strings.TrimSpace(rawSource))
		if scope == types.SourceUnknown {
			return install.PackageRequest{}, fmt.Errorf("unknown source %s", rawSource)
		}
		ref.Scope = scope
	}

	return install.PackageRequest{
		FullPackageRef: types.FullPackageRef{
			PackageRef: ref.PackageRef,
			Version:    version,
			Scope:      ref.Scope,
		},
	}, nil
}
