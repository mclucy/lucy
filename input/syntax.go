// Package input parses package request syntax.
//
// A request has the form "source:name@version". Source and version are
// optional. Its target ecosystem is selected separately by the caller.
package input

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

var ESyntax = errors.New("invalid syntax")

// Parse parses source:name@version. Omitted source and version become
// SourceAuto and VersionAny. Eco remains unspecified until install planning.
func Parse(s string) (types.PackageRequest, error) {
	source, remainder, err := parseSource(strings.TrimSpace(s))
	if err != nil {
		return types.PackageRequest{}, err
	}
	name, version, err := parseNameVersion(remainder)
	if err != nil {
		return types.PackageRequest{}, err
	}
	return types.PackageRequest{
		PackageRef: types.PackageRef{Name: name, Source: source},
		Version:    version,
	}, nil
}

func ToProjectName(s string) types.BarePackageName {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return types.BarePackageName(s)
}

func parseSource(s string) (types.SourceId, string, error) {
	boundary := len(s)
	if at := strings.IndexByte(s, '@'); at >= 0 {
		boundary = at
	}
	colon := strings.IndexByte(s[:boundary], ':')
	if colon < 0 {
		return types.SourceAuto, s, nil
	}
	source := types.ParseSource(strings.ToLower(s[:colon]))
	if source == types.SourceUnknown {
		return types.SourceUnknown, "", fmt.Errorf("%w: unknown source %q", ESyntax, s[:colon])
	}
	return source, s[colon+1:], nil
}

func parseNameVersion(s string) (types.BarePackageName, types.BareVersion, error) {
	parts := strings.Split(s, "@")
	switch len(parts) {
	case 1:
		if parts[0] == "" {
			return "", "", ESyntax
		}
		return types.BarePackageName(parts[0]), types.VersionAny, nil
	case 2:
		if parts[0] == "" || parts[1] == "" || types.BareVersion(parts[1]) == types.VersionNone {
			return "", "", ESyntax
		}
		return types.BarePackageName(parts[0]), types.BareVersion(parts[1]), nil
	default:
		return "", "", ESyntax
	}
}
