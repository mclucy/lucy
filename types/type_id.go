// Package types is a general package for all types used in Lucy.
//
// This package contains ONLY pure domain semantics. It must have no side effects:
//   - NO logging (log.)
//   - NO filesystem access (os.)
//   - NO panics (panic())
//
// All functions should be deterministic and side effect free.
package types

import (
	"strings"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/tui/style"
)

// Ecosystem is an enum of several string constants.
//
// All platform is a package under itself, for example, "fabric/fabric" is a
// valid package, and is equivalent to "fabric". This literal is typically used
// when installing/upgrading a platform itself.
type Ecosystem string

const (
	// special selectors

	// EcoAny is ambiguous but has single-valueness. It does NOT refer
	// to multiple platforms, but rather a single platform that is unknown.
	// Understand this as EcoAny reduces to a definite platform at
	// evaluation. Again, keep in mind that you should not allow it to be
	// explicitly evaluated as multiple platforms.
	EcoAny Ecosystem = ""

	// EcoBare is a special platform that is not satisfied by any platform,
	// but it can satisfy all platforms. It is typically used to indicate the
	// absence of a platform, for example, when a package is not compatible with
	// any platform, or when a package does not require a platform.
	EcoBare Ecosystem = "bare"

	// EcoUnknown is the only constant with no single-valueness, it can
	// refer to multiple platforms other than the ones defined here.
	EcoUnknown Ecosystem = "unknown"

	// vanilla

	EcoMinecraft Ecosystem = "minecraft"
	EcoVanilla             = EcoMinecraft // alias

	// modding platforms

	EcoFabric   Ecosystem = "fabric"
	EcoForge    Ecosystem = "forge"
	EcoNeoforge Ecosystem = "neoforge"

	// others

	EcoBukkit     Ecosystem = "bukkit"     // Bukkit or spigot plugins
	EcoPaper      Ecosystem = "paper"      // Paper and its forks' plugins
	EcoBungeecord Ecosystem = "bungeecord" // Can be consumed by both waterfall and bungeecord itself
	EcoVelocity   Ecosystem = "velocity"
	EcoSponge     Ecosystem = "sponge"
	EcoMcdr       Ecosystem = "mcdr"
)

func (p Ecosystem) Title() string {
	if p == EcoAny {
		return "Any"
	}
	if p.Valid() {
		return strings.ToUpper(string(p)[0:1]) + string(p)[1:]
	}
	return "Unknown"
}

func (p Ecosystem) String() string {
	if p == EcoAny {
		return "any"
	}
	return string(p)
}

// Valid
//
// If a platform can be used in a package id, it is a valid platform.
func (p Ecosystem) Valid() bool {
	switch p {
	case EcoMinecraft, EcoFabric, EcoForge, EcoNeoforge, EcoMcdr, EcoBukkit, EcoAny, EcoBare:
		return true
	}
	return false
}

func (p Ecosystem) IsSearchEcosystem() bool {
	switch p {
	case EcoFabric, EcoForge, EcoNeoforge, EcoBukkit:
		return true
	default:
		return false
	}
}

// Satisfy returns true if p satisfies the requirement of p2.
func (p Ecosystem) Satisfy(p2 Ecosystem) bool {
	// When p2 is PlatformNone, it is satisfied by all platforms.
	if p2 == EcoBare {
		return true
	}
	// PlatformUnknown is not satisfied by any platform, and does not satisfy
	// any platform including itself.
	if p == EcoUnknown || p2 == EcoUnknown {
		return false
	}
	// When p2 is PlatformAny, it is satisfied by all platforms.
	if p2 == EcoAny {
		return true
	}
	// When p is PlatformAny, it does not satisfy any platform except itself.
	if p == EcoAny {
		return false
	}
	if p2 == EcoBukkit && p == EcoPaper {
		return true
	}

	// Trivial cases
	return p == p2
}

// Is is just an alias for `==`, they are fully interchangeable. There's no
// restriction on which one to use.
//
// This function does not represent a mathematical equivalence relation, since
// EcoUnknown should always be unequal to any platform including itself.
// However, rather than using .IsUnknown() function, it is more intuitive to
// just use an equality operator.
//
// This is created to differentiate the meaning of "satisfy" and "is".
// For example, "fabric" satisfies "minecraft", but does not "is" "minecraft".
func (p Ecosystem) Is(p2 Ecosystem) bool {
	return p == p2
}

func (p Ecosystem) IsModding() bool {
	return p == EcoFabric || p == EcoForge || p == EcoNeoforge
}

func DeclaredModdingEcosystemForNode(id RuntimeNodeID) Ecosystem {
	switch id {
	case "fabric":
		return EcoFabric
	case "forge", "arclight", "catserver":
		return EcoForge
	case "neoforge", "youer":
		return EcoNeoforge
	case "mcdr":
		return EcoMcdr
	case "minecraft":
		return EcoMinecraft
	default:
		return EcoBare
	}
}

// IsSelector returns true if the platform is ambiguous and can be resolved
// from server context.
func (p Ecosystem) IsSelector() bool {
	return p == EcoAny
}

// Title Replaces underlines or hyphens with spaces, then capitalize the first
// letter.
func (n BarePackageName) Title() string {
	return style.Capitalize(strings.ReplaceAll(string(n), "-", " "))
}

func (n BarePackageName) String() string {
	return string(n)
}

func (n BarePackageName) Pep8String() string {
	return strings.ReplaceAll(string(n), "-", "_")
}

func (p VersionedPackageRef) String() string {
	return fn.Ternary(
		p.Eco == EcoAny,
		"", string(p.Eco)+"/",
	) +
		string(p.Name) +
		fn.Ternary(
			p.Version == VersionAny,
			"",
			"@"+string(p.Version),
		)
}

func (p VersionedPackageRef) StringFull() string {
	return p.Eco.String() + "/" + p.Name.String() + "@" + p.Version.String()
}

func (p VersionedPackageRef) StringBase() string {
	return string(p.Eco) + "/" + string(p.Name)
}
