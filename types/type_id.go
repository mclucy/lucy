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

	"github.com/mclucy/lucy/terminal/style"
)

// Ecosystem is an enum of several string constants.
//
// All platform is a package under itself, for example, "fabric/fabric" is a
// valid package, and is equivalent to "fabric". This literal is typically used
// when installing/upgrading a platform itself.
type Ecosystem string

const (
	// special selectors

	// EcoUnspecified is ambiguous but has single-valueness. It does NOT refer
	// to multiple platforms, but rather a single platform that is unknown.
	// Understand this as EcoUnspecified reduces to a definite platform at
	// evaluation. Again, keep in mind that you should not allow it to be
	// explicitly evaluated as multiple platforms.
	EcoUnspecified Ecosystem = ""

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

func (e Ecosystem) Title() string {
	if e == EcoUnspecified {
		return "Any"
	}
	if e.Valid() {
		return strings.ToUpper(string(e)[0:1]) + string(e)[1:]
	}
	return "Unknown"
}

func (e Ecosystem) String() string {
	if e == EcoUnspecified {
		return "any"
	}
	return string(e)
}

// Valid
//
// If a platform can be used in a package id, it is a valid platform.
func (e Ecosystem) Valid() bool {
	switch e {
	case EcoMinecraft, EcoFabric, EcoForge, EcoNeoforge, EcoMcdr, EcoBukkit, EcoUnspecified:
		return true
	}
	return false
}

func (e Ecosystem) IsSearchEcosystem() bool {
	switch e {
	case EcoFabric, EcoForge, EcoNeoforge, EcoBukkit:
		return true
	default:
		return false
	}
}

// Satisfy returns true if p satisfies the requirement of p2.
func (e Ecosystem) Satisfy(e2 Ecosystem) bool {
	// When p2 is PlatformNone, it is satisfied by all platforms.
	if e2 == EcoUnspecified {
		return true
	}
	// When p is PlatformAny, it does not satisfy any platform except itself.
	if e == EcoUnspecified {
		return false
	}
	if e2 == EcoBukkit && e == EcoPaper {
		return true
	}

	// Trivial cases
	return e == e2
}

func (e Ecosystem) IsModding() bool {
	return e == EcoFabric || e == EcoForge || e == EcoNeoforge
}

// IsSelector returns true if the platform is ambiguous and can be resolved
// from server context.
func (e Ecosystem) IsSelector() bool {
	return e == EcoUnspecified
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
	version := ""
	if p.Version != VersionAny {
		version = "@" + p.Version.String()
	}
	return p.PackageRef.StringFull() + version
}

// StringFull is a human-facing selected-artifact label. StringBase is the
// stable source-qualified package identity used by graph and provenance keys.
func (p VersionedPackageRef) StringFull() string {
	return p.Eco.String() + "/" + p.PackageRef.StringFull() + "@" + p.Version.String()
}

func (p VersionedPackageRef) StringBase() string {
	return p.PackageRef.StringBase()
}
