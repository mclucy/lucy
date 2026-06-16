package install

import (
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
)

// PackageRequest is the universal desired state descriptor, i.e., the main
// entrance of this package.
// This includes:
//   - via `lucy add`
//   - resolving from manifest
//
// This is data only. Ownership roles such as required/transitive/ignored, and
// relation roles such as dependency/embedded, are supplied by the surrounding
// context rather than stored here.
type PackageRequest struct {
	Ref      types.PackageRef
	Version  types.BareVersion
	Optional bool
	Source   types.SourceId
}

func ParsePackageRequest(s string, bareSource string, optional bool) (
	req PackageRequest,
	err error,
) {
	s = strings.TrimSpace(strings.ToLower(s))
	req = PackageRequest{}

	var ref types.PackageRef
	ref, err = input.ParsePackageRef(s)
	if err != nil {
		return req, err
	}

	var version types.BareVersion
	if len(strings.Split(s, "@")) > 1 {
		version = types.BareVersion(strings.Split(s, "@")[1])
	} else {
		version = types.VersionAny
	}

	var parsedSource types.SourceId
	parsedSource = types.ParseSource(bareSource)

	req.Ref = ref
	req.Version = version
	req.Source = parsedSource
	req.Optional = optional

	return req, nil
}
