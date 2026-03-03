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
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/types"
)

func ToProjectName(s string) types.ProjectName {
	return types.ProjectName(sanitize(s))
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

// Parse is exported to parse a string into a PackageId struct.
func Parse(s string) (id types.PackageId) {
	s = sanitize(s)
	id = types.PackageId{}
	var err error
	id.Platform, id.Name, id.Version, err = parseOperatorAt(s)
	if err != nil {
		if errors.Is(err, ESyntax) {
			panic(err)
		} else {
			logger.Fatal(err)
		}
	}
	logger.Debug("parsed input as package: " + id.StringFull())
	return
}

// parseOperatorAt is called first since '@' operator always occur after '/' (equivalent
// to a lower priority).
func parseOperatorAt(s string) (
	pl types.Platform,
	n types.ProjectName,
	v types.RawVersion,
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
		v = types.RawVersion(split[1])
		if v == types.VersionNone || v == types.VersionAny {
			return "", "", "", ESyntax
		}
	} else {
		return "", "", "", ESyntax
	}

	return
}

func parseOperatorSlash(s string) (
	pl types.Platform,
	n types.ProjectName,
	err error,
) {
	split := strings.Split(s, "/")

	if len(split) == 1 {
		pl = types.PlatformAny
		n = types.ProjectName(split[0])
		if types.Platform(n).Valid() {
			// Remember, all platforms are also valid packages under themselves.
			// This literal is for users to specify the platform itself. See the
			// docs for syntaxtypes.ModLoader for more information.
			pl = types.Platform(n)
			n = types.ProjectName(pl)
		}
	} else if len(split) == 2 {
		pl = types.Platform(split[0])
		if !pl.Valid() {
			return "", "", EPlatform
		}
		n = types.ProjectName(split[1])
	} else {
		return "", "", ESyntax
	}

	return
}
