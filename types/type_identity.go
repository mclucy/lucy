package types

import "strings"

// IdentityEntry defines one canonical identity package.
type IdentityEntry struct {
	Name    BarePackageName
	Eco     Ecosystem
	Aliases []BarePackageName
}

func init() {
	nameToIdentity = make(
		map[BarePackageName]*IdentityEntry,
		len(identityRegistry)*2,
	)
	ecosystemToIdentity = make(
		map[Ecosystem]*IdentityEntry,
		len(identityRegistry),
	)
	for i := range identityRegistry {
		e := &identityRegistry[i]
		nameToIdentity[e.Name] = e
		for _, alias := range e.Aliases {
			nameToIdentity[alias] = e
		}
		if e.Eco != EcoUnspecified {
			ecosystemToIdentity[e.Eco] = e
		}
	}
}

var (
	nameToIdentity      map[BarePackageName]*IdentityEntry // aliases + canonical name → entry
	ecosystemToIdentity map[Ecosystem]*IdentityEntry       // platform → entry (only for non-None)
)

var identityRegistry = []IdentityEntry{
	{
		Name: "minecraft", Eco: EcoMinecraft,
		Aliases: []BarePackageName{"mc"},
	},
	{
		Name: "fabric", Eco: EcoFabric,
		Aliases: []BarePackageName{"fabric-loader"},
	},
	{Name: "forge", Eco: EcoForge, Aliases: nil},
	{Name: "neoforge", Eco: EcoNeoforge, Aliases: nil},
	{
		Name: "mcdreforged", Eco: EcoMcdr,
		Aliases: []BarePackageName{"mcdr"},
	},
	// Server cores — platform is not meaningful here
	// {Name: "paper", Platform: PlatformNone, Aliases: []string{"papermc"}},
	// {Name: "purpur", Platform: PlatformNone, Aliases: nil},
}

// NormalizeIdentityPackage rewrites aliases to their canonical form.
// Identity package names are always lowercase by spec; the lookup is
// case-insensitive on the input name.
// Returns (canonical, true) if the ref is an identity package, (zero, false) otherwise.
func NormalizeIdentityPackage(p PackageRef) (PackageRef, bool) {
	entry, ok := nameToIdentity[BarePackageName(strings.ToLower(string(p.Name)))]
	if !ok {
		return PackageRef{}, false
	}
	return PackageRef{
		Eco:  entry.Eco,
		Name: entry.Name,
	}, true
}

func IsIdentityPackage(p PackageRef) bool {
	_, exists := nameToIdentity[BarePackageName(strings.ToLower(string(p.Name)))]
	return exists
}
