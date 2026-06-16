// Package syntax defines the syntax for specifying packages and platforms.
//
// A package can either be specified by a string in the format of
// "platform/name@version". Only the name is required, both platform and version
// can be omitted.
//
// Valid Examples:
//   - carpet
//   - mcdr/prime-backup
//   - fabric/jade@1.0.0
//   - fabric@12.0
//   - minecraft@1.19 (recommended)
//   - minecraft/minecraft@1.16.5 (= minecraft@1.16.5)
//   - 1.8.9 (= minecraft@1.8.9)
package input

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

var (
	ESyntax   = errors.New("invalid syntax")
	EPlatform = errors.New("invalid platform")
	EIdentity = errors.New("invalid identity package")
)

func ParsePackageRef(s string) (ref types.PackageRef, err error) {
	ref = types.PackageRef{}

	s = strings.TrimSpace(s)
	s = strings.Split(s, "@")[0] // strip and ignore version specifiers

	switch len(strings.Split(s, "/")) {
	case 1:
		ref.Platform = types.PlatformAny
		ref.Name = types.BarePackageName(s)
	case 2:
		ref.Platform = types.PlatformId(strings.Split(s, "/")[0])
		ref.Name = types.BarePackageName(strings.Split(s, "/")[1])
	default:
		return types.PackageRef{}, fmt.Errorf(
			"%w: multiple '/' found in specifier %s, maximum 1 is allowed",
			ESyntax, s,
		)
	}

	return ref, nil
}

// Parse is exported to parse a string into a PackageId struct.
// Returns the parsed PackageId and an error if parsing fails.
func Parse(s string) (id types.VersionedPackageRef, err error) {
	text := strings.TrimSpace(strings.ToLower(s))
	id = types.VersionedPackageRef{}
	id.Platform, id.Name, id.Version, err = parseOperatorAt(text)
	if err != nil {
		return types.VersionedPackageRef{}, err
	}
	identity, ok := types.NormalizeIdentityPackage(id.PackageRef)
	if ok {
		id.PackageRef = identity
	}
	return id, nil
}

func ToProjectName(s string) types.BarePackageName {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return types.BarePackageName(s)
}

// parseOperatorAt is called first since '@' operator always occur after '/' (equivalent
// to a lower priority).
func parseOperatorAt(s string) (
	pl types.PlatformId,
	n types.BarePackageName,
	v types.BareVersion,
	err error,
) {
	split := strings.Split(s, "@")

	pl, n, err = parseOperatorSlash(split[0])
	if err != nil {
		return "", "", "", ESyntax
	}

	if len(split) == 1 {
		v = types.VersionAny
	} else if len(split) == 2 {
		v = types.BareVersion(split[1])
		if v == types.VersionNone {
			return "", "", "", ESyntax
		}
	} else {
		return "", "", "", ESyntax
	}

	return
}

func parseOperatorSlash(s string) (
	pl types.PlatformId,
	n types.BarePackageName,
	err error,
) {
	split := strings.SplitN(s, "/", 2)

	if len(split) == 1 {
		pl = types.PlatformAny
		n = types.BarePackageName(split[0])
		if types.PlatformId(n).Valid() {
			// Remember, all platforms are also valid packages under themselves.
			// This literal is for users to specify the platform itself.
			// This means the user specified a platform name directly.
			pl = types.PlatformId(n)
			n = types.BarePackageName(pl)
		}
	} else {
		pl = types.PlatformId(split[0])
		if !pl.Valid() {
			return "", "", EPlatform
		}
		n = types.BarePackageName(split[1])
	}

	return
}
