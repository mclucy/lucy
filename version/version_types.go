// Package version owns version vocabulary, parsing, ordering, and constraint
// matching for every package dialect that lucy resolves.
package version

// Version is a package version. It may hold a selector during resolution or
// an exact version after resolution.
type Version string

func (v Version) String() string {
	switch v {
	case Any, "":
		return "any"
	case None:
		return "none"
	case Unknown:
		return "unknown"
	case Beta:
		return "beta"
	case Stable:
		return "stable"
	}
	return string(v)
}

func (v Version) CanInfer() bool {
	switch v {
	case Any, Beta, Stable:
		return true
	}
	return false
}

func (v Version) IsInvalid() bool {
	switch v {
	case None, Unknown:
		return true
	}
	return false
}

var (
	Any     Version = "any"
	Beta    Version = "beta"
	Stable  Version = "stable"
	None    Version = "none"
	Unknown Version = "unknown"
)

// Dialect identifies a version grammar together with the ordering and
// constraint semantics that grammar defines.
type Dialect string

const (
	DialectUnknown Dialect = ""

	// DialectFabric is the version grammar of fabric.mod.json metadata.
	// Reference: https://wiki.fabricmc.net/documentation:fabric_mod_json_spec
	DialectFabric Dialect = "fabric"
	// DialectMCDR is the version grammar of mcdreforged.plugin.json metadata.
	// Reference: https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
	DialectMCDR Dialect = "mcdr"
	// DialectMaven is Maven version ordering, used by Forge and NeoForge
	// mods.toml metadata. Reference:
	// https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
	DialectMaven Dialect = "maven"
	// DialectMinecraft is the Minecraft release and snapshot grammar.
	// Reference: https://zh.minecraft.wiki/w/%E7%89%88%E6%9C%AC%E6%A0%BC%E5%BC%8F
	DialectMinecraft Dialect = "minecraft"
	// DialectOpaque treats versions as unordered identifiers. It serves
	// descriptors whose grammar defines no version ranges, such as plugin.yml.
	DialectOpaque Dialect = "opaque"
)

// Valid reports whether d is a dialect defined by this package.
func (d Dialect) Valid() bool {
	switch d {
	case DialectFabric,
		DialectMCDR,
		DialectMaven,
		DialectMinecraft,
		DialectOpaque:
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
	Dialect() Dialect
}
