package dependency

import (
	"strings"

	"github.com/mclucy/lucy/types"
)

// VersionRangeDialect defines the grammar used when parsing a range string.
type VersionRangeDialect uint8

const (
	DialectUnknown VersionRangeDialect = iota
	// DialectNpmSemver represents MCDR plugin metadata dependency ranges.
	// References:
	//   - https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
	//   - https://docs.npmjs.com/about-semantic-versioning
	DialectNpmSemver
	// DialectFabricSemver represents Fabric loader range syntax.
	// Reference: https://wiki.fabricmc.net/documentation:fabric_mod_json_spec
	DialectFabricSemver
	// DialectMavenRange are Maven version ranges in mods.toml used by Forge and NeoForge.
	// References:
	//   - https://docs.minecraftforge.net/en/latest/gettingstarted/modfiles/
	//   - https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
	DialectMavenRange
)

// InferRangeDialect infers the range dialect from package platform.
func InferRangeDialect(platform types.Platform) VersionRangeDialect {
	switch platform {
	case types.PlatformMCDR:
		return DialectNpmSemver
	case types.PlatformFabric:
		return DialectFabricSemver
	case types.PlatformForge, types.PlatformNeoforge:
		return DialectMavenRange
	default:
		return DialectUnknown
	}
}

// ParseRange parses range text using the given dialect.
//
// This parser layer is the intended home for syntax-specific operators such as
// '^' and '~'. It expands these operators into basic comparison constraints
// (>, >=, <, <=, =, !=) so that the evaluator layer stays dialect-agnostic.
func ParseRange(
	raw string,
	dialect VersionRangeDialect,
	scheme types.VersionScheme,
) types.VersionConstraintExpression {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	switch dialect {
	case DialectNpmSemver:
		if scheme != types.Semver {
			return nil
		}
		// MCDR uses space-separated criteria (AND) with operators
		// >=, >, <=, <, =, ==, ^, ~ and wildcard versions.
		// Reference: https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
		return parseMcdrSemverRange(raw)
	case DialectFabricSemver:
		if scheme != types.Semver {
			return nil
		}
		// Fabric semantics: '^' keeps same-major behavior without 0.x special-casing.
		// Reference: https://wiki.fabricmc.net/documentation:fabric_mod_json_spec
		return parseSemverRange(
			raw,
			semverRangeOptions{caretMode: caretModeSameMajor},
		)
	case DialectMavenRange:
		if scheme != types.Semver {
			return nil
		}
		return parseMavenRange(raw)
	default:
		return nil
	}
}

// ParseRanges parses multiple range strings as OR alternatives.
//
// This matches the VersionConstraintExpression design where the outer slice
// represents OR clauses and each inner slice represents AND constraints.
//
// If any item resolves to an unconstrained expression (nil/empty), the result
// is unconstrained (nil), because one OR branch already matches all versions.
func ParseRanges(
	raws []string,
	dialect VersionRangeDialect,
	scheme types.VersionScheme,
) types.VersionConstraintExpression {
	if len(raws) == 0 {
		return nil
	}

	merged := make(types.VersionConstraintExpression, 0, len(raws))
	for _, raw := range raws {
		expr := ParseRange(raw, dialect, scheme)
		if len(expr) == 0 {
			return nil
		}
		merged = append(merged, expr...)
	}

	if len(merged) == 0 {
		return nil
	}
	return merged
}
