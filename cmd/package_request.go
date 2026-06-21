package cmd

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/types"
)

func packageRequestFromInput(raw string, rawSource string) (install.PackageRequest, error) {
	ref, err := input.ParseFullPackageRef(raw)
	if err != nil {
		return install.PackageRequest{}, err
	}

	if rawSource != "" && ref.Scope == types.SourceAuto {
		scope := types.ParseSource(strings.TrimSpace(rawSource))
		if scope == types.SourceUnknown {
			return install.PackageRequest{}, fmt.Errorf("unknown source %s", rawSource)
		}
		ref.Scope = scope
	}

	return install.PackageRequest{FullPackageRef: ref}, nil
}
