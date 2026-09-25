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

// Ecosystem identifies a runtime ecosystem or the unresolved selector used
// before workspace context is available.
type Ecosystem string

const (
	// EcoUnspecified is a single unknown ecosystem, resolved during planning.
	EcoUnspecified Ecosystem = ""

	EcoMinecraft Ecosystem = "minecraft"
	EcoVanilla             = EcoMinecraft

	// Modding ecosystems.

	EcoFabric   Ecosystem = "fabric"
	EcoForge    Ecosystem = "forge"
	EcoNeoforge Ecosystem = "neoforge"

	// Other runtime ecosystems.

	EcoBukkit     Ecosystem = "bukkit"
	EcoPaper      Ecosystem = "paper"
	EcoBungeecord Ecosystem = "bungeecord"
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

// Valid reports whether e is accepted in package identity. EcoUnspecified is
// valid only while a request is awaiting workspace context.
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

// Satisfy reports whether e satisfies required. An unspecified requirement
// accepts any ecosystem; an unspecified receiver matches only that selector.
func (e Ecosystem) Satisfy(e2 Ecosystem) bool {
	if e2 == EcoUnspecified {
		return true
	}
	if e == EcoUnspecified {
		return false
	}
	if e2 == EcoBukkit && e == EcoPaper {
		return true
	}

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

// Title formats a package name for display.
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
