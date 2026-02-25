package detector

import (
	"strings"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/types"
)

// parseNpmVersionRange parses a simplified npm/semver version range string into
// a VersionConstraintExpression.
//
// Simplified rules (on purpose):
//   - Only a single token is supported. The input is trimmed and only the first
//     space-separated token is considered. Complex expressions (space-separated
//     ANDs, `||` ORs, hyphen ranges like `1.0.0 - 1.2.0`) are ignored.
//   - Supported prefixes/operators on the single token:
//     ^ (caret)  -> expanded to a lower bound (>=) and an exclusive upper bound (<)
//     ~ (tilde)  -> expanded to a lower bound (>=) and an exclusive upper bound (<)
//     >=, <=, >, <, =
//     no prefix  -> treated as exact match (=)
//   - Wildcards: `*` or `x` are treated as "no constraint" and result in nil.
//
// Return value:
//   - returns nil when the input is empty, a wildcard, or cannot produce any
//     meaningful constraints.
//   - otherwise returns a VersionConstraintExpression containing a single inner
//     slice (because the full npm range grammar with OR/AND is intentionally
//     unsupported here).
func parseNpmVersionRange(s string) types.VersionConstraintExpression {
	s = strings.TrimSpace(s)
	if s == "*" || s == "x" || s == "" {
		return nil
	}

	// Only accept the first token (prefix + version). We deliberately ignore complex
	// expressions (spaces, AND, OR, hyphen ranges) since they are not needed here.
	if idx := strings.IndexAny(s, " \t"); idx != -1 {
		s = s[:idx]
	}

	constraints := parseSingleNpmVersion(s)
	if len(constraints) == 0 {
		return nil
	}
	return types.VersionConstraintExpression{constraints}
}

// parseSingleNpmVersion parses a single npm version constraint.
// Returns a slice because ^ and ~ operators expand to multiple constraints.
func parseSingleNpmVersion(version string) []types.VersionConstraint {
	version = strings.TrimSpace(version)

	// Handle wildcard
	if version == "*" || version == "x" {
		return nil
	}

	// Handle caret (^) operator
	if strings.HasPrefix(version, "^") {
		version = strings.TrimPrefix(version, "^")
		return parseCaretRange(version)
	}

	// Handle tilde (~) operator
	if strings.HasPrefix(version, "~") {
		version = strings.TrimPrefix(version, "~")
		return parseTildeRange(version)
	}

	// Handle standard comparison operators
	var op types.VersionOperator
	if strings.HasPrefix(version, ">=") {
		op = types.OpGte
		version = strings.TrimPrefix(version, ">=")
	} else if strings.HasPrefix(version, "<=") {
		op = types.OpLte
		version = strings.TrimPrefix(version, "<=")
	} else if strings.HasPrefix(version, ">") {
		op = types.OpGt
		version = strings.TrimPrefix(version, ">")
	} else if strings.HasPrefix(version, "<") {
		op = types.OpLt
		version = strings.TrimPrefix(version, "<")
	} else if strings.HasPrefix(version, "=") {
		op = types.OpEq
		version = strings.TrimPrefix(version, "=")
	} else {
		// Default to equality
		op = types.OpEq
	}

	version = strings.TrimSpace(version)
	parsedVer := dependency.Parse(types.RawVersion(version), types.Semver)

	return []types.VersionConstraint{
		{Value: parsedVer, Operator: op},
	}
}

// parseCaretRange handles ^ operator
// ^2.2.1 => >=2.2.1 <3.0.0
// ^0.1.0 => >=0.1.0 <0.2.0 (special for 0.x)
// ^0.0.3 => >=0.0.3 <0.0.4 (special for 0.0.x)
func parseCaretRange(version string) []types.VersionConstraint {
	parsedVer := dependency.Parse(types.RawVersion(version), types.Semver)

	var upperBound types.ComparableVersion
	if parsedVer.Major == 0 {
		if parsedVer.Minor == 0 {
			// ^0.0.x => >=0.0.x <0.0.(x+1)
			upperBound = types.ComparableVersion{
				Scheme: types.Semver,
				Major:  0,
				Minor:  0,
				Patch:  parsedVer.Patch + 1,
			}
		} else {
			// ^0.x.y => >=0.x.y <0.(x+1).0
			upperBound = types.ComparableVersion{
				Scheme: types.Semver,
				Major:  0,
				Minor:  parsedVer.Minor + 1,
				Patch:  0,
			}
		}
	} else {
		// ^x.y.z => >=x.y.z <(x+1).0.0
		upperBound = types.ComparableVersion{
			Scheme: types.Semver,
			Major:  parsedVer.Major + 1,
			Minor:  0,
			Patch:  0,
		}
	}

	return []types.VersionConstraint{
		{Value: parsedVer, Operator: types.OpGte},
		{Value: upperBound, Operator: types.OpLt},
	}
}

// parseTildeRange handles ~ operator
// ~2.2.0 => >=2.2.0 <2.3.0
func parseTildeRange(version string) []types.VersionConstraint {
	parsedVer := dependency.Parse(types.RawVersion(version), types.Semver)

	upperBound := types.ComparableVersion{
		Scheme: types.Semver,
		Major:  parsedVer.Major,
		Minor:  parsedVer.Minor + 1,
		Patch:  0,
	}

	return []types.VersionConstraint{
		{Value: parsedVer, Operator: types.OpGte},
		{Value: upperBound, Operator: types.OpLt},
	}
}
