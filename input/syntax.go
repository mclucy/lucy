// Package input parses package request syntax.
//
// A request has the form "provider:name@version". Provider and version are
// optional. Its target ecosystem is selected separately by the caller.
package input

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

var ESyntax = errors.New("invalid syntax")

// Parse parses provider:name@version. An omitted provider lets routing choose;
// an omitted version becomes VersionAny. The ecosystem remains unspecified
// until install planning.
func Parse(s string) (catalog.VersionedRef, error) {
	provider, remainder, err := parseProvider(strings.TrimSpace(s))
	if err != nil {
		return catalog.VersionedRef{}, err
	}
	name, version, err := parseNameVersion(remainder)
	if err != nil {
		return catalog.VersionedRef{}, err
	}
	return catalog.VersionedRef{
		Name: name, Provider: provider,
		Version: version,
	}, nil
}

func ToProjectName(s string) catalog.Name {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return catalog.Name(s)
}

func parseProvider(s string) (catalog.Provider, string, error) {
	boundary := len(s)
	if at := strings.IndexByte(s, '@'); at >= 0 {
		boundary = at
	}
	colon := strings.IndexByte(s[:boundary], ':')
	if colon < 0 {
		return catalog.ProviderUnspecified, s, nil
	}
	provider, ok := catalog.ParseProvider(strings.ToLower(s[:colon]))
	if !ok || provider == catalog.ProviderUnspecified {
		return catalog.ProviderUnknown, "", fmt.Errorf("%w: unknown provider %q", ESyntax, s[:colon])
	}
	return provider, s[colon+1:], nil
}

func parseNameVersion(s string) (catalog.Name, versionpkg.Version, error) {
	parts := strings.Split(s, "@")
	switch len(parts) {
	case 1:
		if parts[0] == "" {
			return "", "", ESyntax
		}
		return catalog.Name(parts[0]), versionpkg.Any, nil
	case 2:
		if parts[0] == "" || parts[1] == "" || versionpkg.Version(parts[1]) == versionpkg.None {
			return "", "", ESyntax
		}
		return catalog.Name(parts[0]), versionpkg.Version(parts[1]), nil
	default:
		return "", "", ESyntax
	}
}
