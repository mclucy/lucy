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
package syntax

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

func ToProjectName(s string) types.PackageName {
	return types.PackageName(sanitize(s))
}

// sanitize tolerates some common interchangeability between characters. This
// includes underscores, chinese full stops, and backslashes. It also converts
// uppercase characters to lowercase.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, char := range s {
		switch {
		case char == '_':
			b.WriteByte('-')
		case char == '\\':
			b.WriteByte('/')
		case char == '。':
			b.WriteByte('.')
		case 'A' <= char && char <= 'Z':
			b.WriteRune(char + 'a' - 'A')
		default:
			b.WriteRune(char)
		}
	}

	return b.String()
}

var (
	ESyntax   = errors.New("invalid syntax")
	EPlatform = errors.New("invalid platform")
)

func ParsePackageRequest(s string, bareSource string, optional bool) (
	req types.PackageRequest,
	err error,
) {
	s = sanitize(s)
	req = types.PackageRequest{}

	var ref types.PackageRef
	ref, err = ParsePackageRef(s)
	if err != nil {
		return req, err
	}

	var version types.BareVersion
	if len(strings.Split(s, "@")) > 1 {
		version = types.BareVersion(strings.Split(s, "@")[1])
	} else {
		version = types.VersionAny
	}

	var parsedSource types.Source
	parsedSource = types.ParseSource(bareSource)

	req.Ref = ref
	req.Version = version
	req.Source = parsedSource
	req.Optional = optional

	return req, nil
}

func ParsePackageRef(s string) (ref types.PackageRef, err error) {
	ref = types.PackageRef{}

	s = strings.TrimSpace(s)
	s = sanitize(s)
	s = strings.Split(s, "@")[0] // strip and ignore version specifiers

	switch len(strings.Split(s, "/")) {
	case 1:
		ref.Platform = types.PlatformAny
		ref.Name = types.PackageName(s)
	case 2:
		ref.Platform = types.Platform(strings.Split(s, "/")[0])
		ref.Name = types.PackageName(strings.Split(s, "/")[1])
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
func Parse(s string) (id types.PackageId, err error) {
	s = sanitize(s)
	id = types.PackageId{}
	id.Platform, id.Name, id.Version, err = parseOperatorAt(s)
	if err != nil {
		return types.PackageId{}, err
	}
	id.NormalizeIdentityPackage()
	return id, nil
}

// parseOperatorAt is called first since '@' operator always occur after '/' (equivalent
// to a lower priority).
func parseOperatorAt(s string) (
	pl types.Platform,
	n types.PackageName,
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
	pl types.Platform,
	n types.PackageName,
	err error,
) {
	split := strings.Split(s, "/")

	if len(split) == 1 {
		pl = types.PlatformAny
		n = types.PackageName(split[0])
		if types.Platform(n).Valid() {
			// Remember, all platforms are also valid packages under themselves.
			// This literal is for users to specify the platform itself.
			// This means the user specified a platform name directly.
			pl = types.Platform(n)
			n = types.PackageName(pl)
		}
	} else if len(split) == 2 {
		pl = types.Platform(split[0])
		if !pl.Valid() {
			return "", "", EPlatform
		}
		n = types.PackageName(split[1])
	} else {
		return "", "", ESyntax
	}

	return
}
