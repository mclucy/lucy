// Package types is a general package for all types used in Lucy.
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
	AnyPlatform     Platform = "" // AnyPlatform is ambiguous but has single-valueness. It does NOT refer to multiple platforms, but rather a single platform that is unknown. Understand this as AnyPlatform reduces to a definite platform at evaluation. Again, keep in mind that you should not allow it to be explicitly evaluated as multiple platforms.
	Minecraft       Platform = "minecraft"
	Vanilla         Platform = Minecraft // Alias for Minecraft
	Fabric          Platform = "fabric"
	Forge           Platform = "forge"
	Neoforge        Platform = "neoforge"
	Mcdr            Platform = "mcdr"
	UnknownPlatform Platform = "unknown" // UnknownPlatform is the only constant with no single-valueness, it can refer to multiple platforms other than the ones defined here.
)

func (p Platform) Title() string {
	if p == AnyPlatform {
		return "Any"
	}
	if p.Valid() {
		return strings.ToUpper(string(p)[0:1]) + string(p)[1:]
	}
	return "Unknown"
}

func (p Platform) String() string {
	if p == AnyPlatform {
		return "any"
	}
	return string(p)
}

// Valid should be edited if you added a new platform.
func (p Platform) Valid() bool {
	switch p {
	case Minecraft, Fabric, Forge, Neoforge, Mcdr, AnyPlatform:
		return true
	}
	return false
}

// Satisfy returns true if p satisfies the requirement of p2.
func (p Platform) Satisfy(p2 Platform) bool {
	// UnknownPlatform is not satisfied by any platform, and does not satisfy
	// any platform including itself.
	if p == UnknownPlatform || p2 == UnknownPlatform {
		return false
	}
	// When p2 is AnyPlatform, it is satisfied by all platforms.
	if p2 == AnyPlatform {
		return true
	}
	// When p is AnyPlatform, it does not satisfy any platform except itself.
	if p == AnyPlatform {
		return false
	}
	// Trivial cases
	return p == p2
}

// Is is just an alias for `==`, they are fully interchangeable. There's no
// restriction on which one to use.
//
// This function does not represent a mathematical equivalence relation, since
// UnknownPlatform should always be unequal to any platform including itself.
// However, rather than using .IsUnknown() function, it is more intuitive to
// just use an equality operator.
//
// This is created to differentiate the meaning of "satisfy" and "is".
// For example, "fabric" satisfies "minecraft", but does not "is" "minecraft".
func (p Platform) Is(p2 Platform) bool {
	return p == p2
}

func (p Platform) IsModding() bool {
	return p == Fabric || p == Forge || p == Neoforge
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
		p.Platform == AnyPlatform,
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
	"minecraft":     Minecraft,
	"mc":            Minecraft,
	"fabric":        Fabric,
	"fabric-loader": Fabric,
	"forge":         Forge,
	"neoforge":      Neoforge,
	"mcdreforged":   Mcdr,
	"mcdr":          Mcdr,
}

var canonicalIdentityPackageByPlatform = map[Platform]ProjectName{
	Minecraft: "minecraft",
	Fabric:    "fabric",
	Forge:     "forge",
	Neoforge:  "neoforge",
	Mcdr:      "mcdreforged",
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

	if _, valid := platformByIdentityPackage[p.Name]; !valid {
		return ErrInvalidPlatformPackage(p)
	}

	return nil
}

func (p PackageId) NormalizeIdentityPackage() {
	if !p.IsIdentityPackage() {
		return
	}

	canonicalName, exists := canonicalIdentityPackageByPlatform[p.Platform]
	if exists || p.Name == canonicalName {
		return
	}

	p.Name = canonicalName
	if p.Version.CanInfer() {
		p.Version = VersionCompatible
	}

	return
}

func (p PackageId) IdentityToPlatform() Platform {
	platform, exists := platformByIdentityPackage[p.Name]
	if !exists {
		return UnknownPlatform
	}
	return platform
}
