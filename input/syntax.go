// Package input defines the syntax for specifying packages and platforms.
//
// A package can either be specified by a string in the format of
// "scope:platform/name@version". Only the name is required; scope, platform,
// and version can all be omitted.
//
// The operators, from lowest to highest priority:
//
//	:   scope delimiter     (outermost — split first)
//	@   version delimiter
//	/   platform delimiter  (innermost — split last)
//
// The ':' operator only acts on the leftmost colon that appears before any '/'
// or '@'. Colons inside the version (e.g., Maven "group:artifact:version"
// coordinates after '@') are preserved.
//
// Valid Examples:
//   - carpet
//   - mcdr/prime-backup
//   - fabric/jade@1.0.0
//   - fabric@12.0
//   - minecraft@1.19 (recommended)
//   - minecraft/minecraft@1.16.5 (= minecraft@1.16.5)
//   - 1.8.9 (= minecraft@1.8.9)
//   - modrinth:fabric/jade@1.0.0
//   - modrinth:jade
//   - auto:fabric-api
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

// Parse parses a scoped package specifier ("scope:platform/name@version") into a
// ScopedPackageRef and a BareVersion. An omitted scope defaults to SourceAuto;
// an omitted version defaults to VersionAny. Identity aliases are normalized
// the same way as in ParsePackageRef.
func Parse(s string) (ref types.ScopedPackageRef, version types.BareVersion, err error) {
	text := strings.TrimSpace(s)
	ref = types.ScopedPackageRef{}
	version = types.VersionAny

	scope, remainder, err := parseOperatorColon(text)
	if err != nil {
		return types.ScopedPackageRef{}, "", err
	}

	pl, n, v, err := parseOperatorAt(remainder)
	if err != nil {
		return types.ScopedPackageRef{}, "", err
	}

	ref.PackageRef = types.PackageRef{Platform: pl, Name: n}
	ref.Scope = scope
	version = v

	if identity, ok := types.NormalizeIdentityPackage(ref.PackageRef); ok {
		ref.PackageRef = identity
	}
	return ref, version, nil
}

func ParseFullPackageRef(s string) (types.FullPackageRef, error) {
	ref, version, err := Parse(s)
	if err != nil {
		return types.FullPackageRef{}, err
	}
	return types.FullPackageRef{
		PackageRef: ref.PackageRef,
		Version:    version,
		Scope:      ref.Scope,
	}, nil
}

func ToProjectName(s string) types.BarePackageName {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return types.BarePackageName(s)
}

// parseOperatorColon splits on the scope delimiter ':' — the outermost operator.
// Only the leftmost ':' before the first '/' or '@' is the delimiter; colons
// after '@' (Maven version coordinates) are preserved. No ':' defaults to
// SourceAuto.
func parseOperatorColon(s string) (
	scope types.SourceId,
	remainder string,
	err error,
) {
	boundary := len(s)
	if i := strings.IndexByte(s, '/'); i >= 0 && i < boundary {
		boundary = i
	}
	if i := strings.IndexByte(s, '@'); i >= 0 && i < boundary {
		boundary = i
	}

	colonIdx := strings.IndexByte(s[:boundary], ':')
	if colonIdx < 0 {
		return types.SourceAuto, s, nil
	}

	scope = types.ParseSource(strings.ToLower(s[:colonIdx]))
	if scope == types.SourceUnknown {
		return types.SourceUnknown, "", fmt.Errorf(
			"%w: unknown source %q",
			ESyntax, s[:colonIdx],
		)
	}
	return scope, s[colonIdx+1:], nil
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
		if candidate := types.PlatformId(strings.ToLower(split[0])); candidate.Valid() {
			pl = candidate
			n = types.BarePackageName(pl)
		}
	} else {
		pl = types.PlatformId(strings.ToLower(split[0]))
		if !pl.Valid() {
			return "", "", EPlatform
		}
		n = types.BarePackageName(split[1])
	}

	return
}
