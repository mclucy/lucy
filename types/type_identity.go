package types

// IdentityEntry defines one canonical identity package.
type IdentityEntry struct {
	Name     BarePackageName
	Platform PlatformId
	Aliases  []BarePackageName
}

func init() {
	nameToIdentity = make(
		map[BarePackageName]*IdentityEntry,
		len(identityRegistry)*2,
	)
	platformToIdentity = make(
		map[PlatformId]*IdentityEntry,
		len(identityRegistry),
	)
	for i := range identityRegistry {
		e := &identityRegistry[i]
		nameToIdentity[e.Name] = e
		for _, alias := range e.Aliases {
			nameToIdentity[alias] = e
		}
		if e.Platform != PlatformNone {
			platformToIdentity[e.Platform] = e
		}
	}
}

var (
	nameToIdentity     map[BarePackageName]*IdentityEntry // aliases + canonical name → entry
	platformToIdentity map[PlatformId]*IdentityEntry      // platform → entry (only for non-None)
)

var identityRegistry = []IdentityEntry{
	{
		Name: "minecraft", Platform: PlatformMinecraft,
		Aliases: []BarePackageName{"mc"},
	},
	{
		Name: "fabric", Platform: PlatformFabric,
		Aliases: []BarePackageName{"fabric-loader"},
	},
	{Name: "forge", Platform: PlatformForge, Aliases: nil},
	{Name: "neoforge", Platform: PlatformNeoforge, Aliases: nil},
	{
		Name: "mcdreforged", Platform: PlatformMCDR,
		Aliases: []BarePackageName{"mcdr"},
	},
	// Server cores — platform is not meaningful here
	// {Name: "paper", Platform: PlatformNone, Aliases: []string{"papermc"}},
	// {Name: "purpur", Platform: PlatformNone, Aliases: nil},
}

// NormalizeIdentityPackage rewrites aliases to their canonical form.
// Returns (canonical, true) if the ref is an identity package, (zero, false) otherwise.
func NormalizeIdentityPackage(p PackageRef) (PackageRef, bool) {
	entry, ok := nameToIdentity[p.Name]
	if !ok {
		return PackageRef{}, false
	}
	return PackageRef{
		Platform: entry.Platform,
		Name:     entry.Name,
	}, true
}

func IsIdentityPackage(p PackageRef) bool {
	_, exists := nameToIdentity[p.Name]
	return exists
}
