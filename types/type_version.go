package types

// Version is a package version. It may hold a selector during resolution or
// an exact version after resolution.
type Version string

func (v Version) String() string {
	switch v {
	case VersionAny, "":
		return "any"
	case VersionNone:
		return "none"
	case VersionUnknown:
		return "unknown"
	case VersionBeta:
		return "beta"
	case VersionStable:
		return "stable"
	}
	return string(v)
}

func (v Version) CanInfer() bool {
	switch v {
	case VersionAny, VersionBeta, VersionStable:
		return true
	}
	return false
}

func (v Version) IsInvalid() bool {
	switch v {
	case VersionNone, VersionUnknown:
		return true
	}
	return false
}

var (
	VersionAny     Version = "any"
	VersionBeta    Version = "beta"
	VersionStable  Version = "stable"
	VersionNone    Version = "none"
	VersionUnknown Version = "unknown"
)

// VersionDialect identifies a version grammar together with the ordering and
// constraint semantics that grammar defines.
type VersionDialect string

const (
	VersionDialectUnknown VersionDialect = ""

	// VersionDialectFabric is the version grammar of fabric.mod.json
	// metadata. Reference: https://wiki.fabricmc.net/documentation:fabric_mod_json_spec
	VersionDialectFabric VersionDialect = "fabric"
	// VersionDialectMCDR is the version grammar of mcdreforged.plugin.json
	// metadata. Reference: https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
	VersionDialectMCDR VersionDialect = "mcdr"
	// VersionDialectMaven is Maven version ordering, used by Forge and
	// NeoForge mods.toml metadata. Reference:
	// https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
	VersionDialectMaven VersionDialect = "maven"
	// VersionDialectMinecraft is the Minecraft release and snapshot grammar.
	// Reference: https://zh.minecraft.wiki/w/%E7%89%88%E6%9C%AC%E6%A0%BC%E5%BC%8F
	VersionDialectMinecraft VersionDialect = "minecraft"
	// VersionDialectOpaque treats versions as unordered identifiers. It serves
	// descriptors whose grammar defines no version ranges, such as plugin.yml.
	VersionDialectOpaque VersionDialect = "opaque"
)

// Valid reports whether d is a dialect defined by this package.
func (d VersionDialect) Valid() bool {
	switch d {
	case VersionDialectFabric,
		VersionDialectMCDR,
		VersionDialectMaven,
		VersionDialectMinecraft,
		VersionDialectOpaque:
		return true
	default:
		return false
	}
}

// ResolvableVersion is a parsed version that belongs to exactly one dialect.
//
// A nil ResolvableVersion represents an invalid or unparseable version.
// Implementations reject comparisons against a different dialect.
type ResolvableVersion interface {
	Compare(v2 ResolvableVersion) (int, bool)
	Validate() bool
	Dialect() VersionDialect
}
