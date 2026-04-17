// Package types is a general package for all types used in Lucy.
//
// This package contains ONLY pure domain semantics. It must have no side effects:
//   - NO logging (logger.)
//   - NO filesystem access (os.)
//   - NO panics (panic())
//
// All functions should be deterministic and side-effect free.
package types

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/tools"
)

// Platform is an enum of several string constants.
//
// All platform is a package under itself, for example, "fabric/fabric" is a
// valid package, and is equivalent to "fabric". This literal is typically used
// when installing/upgrading a platform itself.
type Platform string

const (
	PlatformAny        Platform = "" // PlatformAny is ambiguous but has single-valueness. It does NOT refer to multiple platforms, but rather a single platform that is unknown. Understand this as PlatformAny reduces to a definite platform at evaluation. Again, keep in mind that you should not allow it to be explicitly evaluated as multiple platforms.
	PlatformMinecraft  Platform = "minecraft"
	PlatformVanilla             = PlatformMinecraft // Alias for Minecraft
	PlatformFabric     Platform = "fabric"
	PlatformForge      Platform = "forge"
	PlatformNeoforge   Platform = "neoforge"
	PlatformMCDR       Platform = "mcdr"
	PlatformBukkit     Platform = "bukkit" // Can be comsumed by paper/spigot/craftbukkit/etc.
	PlatformSponge     Platform = "sponge"
	PlatformVelocity   Platform = "velocity"
	PlatformBungeecord Platform = "bungeecord" // Can be consumed by both waterfall and bungeecord itself
	PlatformNone       Platform = "none"       // PlatformNone is a special platform that is not satisfied by any platform, but it can satisfy all platforms. It is typically used to indicate the absence of a platform, for example, when a package is not compatible with any platform, or when a package does not require a platform.
	PlatformUnknown    Platform = "unknown"    // PlatformUnknown is the only constant with no single-valueness, it can refer to multiple platforms other than the ones defined here.
)

func (p Platform) Title() string {
	if p == PlatformAny {
		return "Any"
	}
	if p.Valid() {
		return strings.ToUpper(string(p)[0:1]) + string(p)[1:]
	}
	return "Unknown"
}

func (p Platform) String() string {
	if p == PlatformAny {
		return "any"
	}
	return string(p)
}

// Valid
//
// If a platform can be used in a package id, it is a valid platform.
func (p Platform) Valid() bool {
	switch p {
	case PlatformMinecraft, PlatformFabric, PlatformForge, PlatformNeoforge, PlatformMCDR, PlatformAny, PlatformNone:
		return true
	}
	return false
}

// Satisfy returns true if p satisfies the requirement of p2.
func (p Platform) Satisfy(p2 Platform) bool {
	// When p2 is PlatformNone, it is satisfied by all platforms.
	if p2 == PlatformNone {
		return true
	}
	// PlatformUnknown is not satisfied by any platform, and does not satisfy
	// any platform including itself.
	if p == PlatformUnknown || p2 == PlatformUnknown {
		return false
	}
	// When p2 is PlatformAny, it is satisfied by all platforms.
	if p2 == PlatformAny {
		return true
	}
	// When p is PlatformAny, it does not satisfy any platform except itself.
	if p == PlatformAny {
		return false
	}
	// Trivial cases
	return p == p2
}

// Is is just an alias for `==`, they are fully interchangeable. There's no
// restriction on which one to use.
//
// This function does not represent a mathematical equivalence relation, since
// PlatformUnknown should always be unequal to any platform including itself.
// However, rather than using .IsUnknown() function, it is more intuitive to
// just use an equality operator.
//
// This is created to differentiate the meaning of "satisfy" and "is".
// For example, "fabric" satisfies "minecraft", but does not "is" "minecraft".
func (p Platform) Is(p2 Platform) bool {
	return p == p2
}

func (p Platform) IsModding() bool {
	return p == PlatformFabric || p == PlatformForge || p == PlatformNeoforge
}

func DeclaredModdingPlatformForNode(id RuntimeNodeID) Platform {
	switch id {
	case "fabric":
		return PlatformFabric
	case "forge", "arclight":
		return PlatformForge
	case "neoforge", "youer":
		return PlatformNeoforge
	case "mcdr":
		return PlatformMCDR
	case "minecraft":
		return PlatformMinecraft
	default:
		return PlatformNone
	}
}

// CanInfer returns true if the platform is ambiguous and can be resolved
// from server context.
func (p Platform) CanInfer() bool {
	return p == PlatformAny
}

// ProjectName is the slug of the package, using hyphens as separators. For example,
// "fabric-api".
//
// It is non-case-sensitive, though lowercase is recommended. Underlines '_' are
// equivalent to hyphens.
//
// A slug from an upstream API is preferred, if possible. Otherwise, the slug is
// obtained from the executable file. No exceptions since a package must either
// exist on a remote API or user's local files.
type ProjectName string

// Title Replaces underlines or hyphens with spaces, then capitalize the first
// letter.
func (n ProjectName) Title() string {
	return tools.Capitalize(strings.ReplaceAll(string(n), "-", " "))
}

func (n ProjectName) String() string {
	return string(n)
}

func (n ProjectName) Pep8String() string {
	return strings.ReplaceAll(string(n), "-", "_")
}

type PackageId struct {
	Platform Platform
	Name     ProjectName
	Version  RawVersion
}

func (p PackageId) NewPackage() Package {
	return Package{
		Id: PackageId{
			Platform: p.Platform,
			Name:     p.Name,
			Version:  p.Version,
		},
	}
}

func (p PackageId) String() string {
	return tools.Ternary(
		p.Platform == PlatformAny,
		"", string(p.Platform)+"/",
	) +
		string(p.Name) +
		tools.Ternary(
			p.Version == VersionAny,
			"",
			"@"+string(p.Version),
		)
}

func (p PackageId) StringFull() string {
	return p.Platform.String() + "/" + p.StringNameVersion()
}

func (p PackageId) StringNameVersion() string {
	return string(p.Name) + "@" + p.Version.String()
}

func (p PackageId) StringPlatformName() string {
	return string(p.Platform) + "/" + string(p.Name)
}

var platformByIdentityPackage = map[ProjectName]Platform{
	"minecraft":     PlatformMinecraft,
	"mc":            PlatformMinecraft,
	"fabric":        PlatformFabric,
	"fabric-loader": PlatformFabric,
	"forge":         PlatformForge,
	"neoforge":      PlatformNeoforge,
	"mcdreforged":   PlatformMCDR,
	"mcdr":          PlatformMCDR,
}

var canonicalIdentityPackageByPlatform = map[Platform]ProjectName{
	PlatformMinecraft: "minecraft",
	PlatformFabric:    "fabric",
	PlatformForge:     "forge",
	PlatformNeoforge:  "neoforge",
	PlatformMCDR:      "mcdreforged",
}

func (p PackageId) IsIdentityPackage() bool {
	_, exists := platformByIdentityPackage[p.Name]
	return exists
}

func (p PackageId) IsValidIdentityPackage() error {
	if !p.IsIdentityPackage() {
		return nil
	}

	ErrInvalidPlatformPackage := func(p PackageId) error {
		return fmt.Errorf(
			"mismatch in an identity package: %s under %s",
			p.Name,
			p.Platform,
		)
	}

	// Check if platform was explicitly specified and mismatches the identity package's platform
	if p.Platform != PlatformAny {
		expectedPlatform, _ := platformByIdentityPackage[p.Name]
		if p.Platform != expectedPlatform {
			return ErrInvalidPlatformPackage(p)
		}
	}

	return nil
}

func (p *PackageId) NormalizeIdentityPackage() {
	if !p.IsIdentityPackage() {
		return
	}

	platform := p.Platform
	if platform == PlatformAny {
		inferred, exists := platformByIdentityPackage[p.Name]
		if !exists {
			return
		}
		platform = inferred
		p.Platform = platform
	}

	canonicalName, exists := canonicalIdentityPackageByPlatform[platform]
	if !exists {
		return
	}

	if p.Name != canonicalName {
		p.Name = canonicalName
		if p.Version.CanInfer() {
			p.Version = VersionCompatible
		}
	}
}

func (p PackageId) IdentityToPlatform() Platform {
	platform, exists := platformByIdentityPackage[p.Name]
	if !exists {
		return PlatformUnknown
	}
	return platform
}
