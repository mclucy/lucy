// Package input parses package request syntax.
//
// A request has the form "provider:name@version". Provider and version are
// optional. Its target ecosystem is selected separately by the caller.
package input

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

var ESyntax = errors.New("invalid syntax")

// Parse parses provider:name@version. An omitted provider lets routing choose;
// an omitted version becomes VersionAny. The ecosystem remains unspecified
// until install planning.
func Parse(s string) (types.VersionedPackageRef, error) {
	provider, remainder, err := parseProvider(strings.TrimSpace(s))
	if err != nil {
		return types.VersionedPackageRef{}, err
	}
	name, version, err := parseNameVersion(remainder)
	if err != nil {
		return types.VersionedPackageRef{}, err
	}
	return types.VersionedPackageRef{
		Name: name, Provider: provider,
		Version: version,
	}, nil
}

func ToProjectName(s string) types.PackageName {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return types.PackageName(s)
}

func parseProvider(s string) (types.Provider, string, error) {
	boundary := len(s)
	if at := strings.IndexByte(s, '@'); at >= 0 {
		boundary = at
	}
	colon := strings.IndexByte(s[:boundary], ':')
	if colon < 0 {
		return types.ProviderUnspecified, s, nil
	}
	provider, ok := types.ParseProvider(strings.ToLower(s[:colon]))
	if !ok || provider == types.ProviderUnspecified {
		return types.ProviderUnknown, "", fmt.Errorf("%w: unknown provider %q", ESyntax, s[:colon])
	}
	return provider, s[colon+1:], nil
}

func parseNameVersion(s string) (types.PackageName, types.Version, error) {
	parts := strings.Split(s, "@")
	switch len(parts) {
	case 1:
		if parts[0] == "" {
			return "", "", ESyntax
		}
		return types.PackageName(parts[0]), types.VersionAny, nil
	case 2:
		if parts[0] == "" || parts[1] == "" || types.Version(parts[1]) == types.VersionNone {
			return "", "", ESyntax
		}
		return types.PackageName(parts[0]), types.Version(parts[1]), nil
	default:
		return "", "", ESyntax
	}
}
