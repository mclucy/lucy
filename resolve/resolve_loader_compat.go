package resolve

import (
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

func fmlLoader(target Target) bool {
	return target.Product == serverpkg.ProductForge ||
		target.Product == serverpkg.ProductNeoForge
}

func matchesLoaderCompatibility(
	raw string,
	module string,
	target Target,
	dialect versionpkg.Dialect,
) (bool, error) {
	for _, compatible := range loaderCompatibilityVersions(module, target) {
		matched, err := versionpkg.Match(raw, compatible, dialect)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

// loaderCompatibilityVersions mirrors the built-in version support matrices of
// Forge and NeoForge. A loader accepts a dependency when either the selected
// version or one of the module's compatibility versions satisfies the range.
// The entries and their exact Minecraft gates come from the FMLLoader classes
// shipped with the loader:
//
//   - NeoForge FML 1.0.16: 1.19.2 -> javafml 42, minecraft 1.19.1, forge 42.0.9
//   - NeoForge FML 4.0.x:  1.21.1 -> minecraft 1.21, neoforge 21.0.166
//   - NeoForge FML 11.0.16: no entries; 11.0.23 and 12.x cover 1.21.8
//     -> minecraft 1.21.7, neoforge 21.7.26-beta
//   - Forge FML 43.x:      1.19.2 -> javafml 42, minecraft 1.19.1, forge 42.0.9
//   - Forge FML 47.4.x:    1.20.1 -> forge 47.1.79
//   - Forge FML 52.x:      1.21.1 -> javafml 51, minecraft 1.21, forge 51.0.33
//   - Forge FML 54.x and 61.x keep the 1.21.1 gate, so they have no entries
//     on their own Minecraft version.
//
// A compatibility entry can satisfy a dependency even when no selected package
// provides the module, because the loader's matrix substitutes a version for
// modules such as languageloader.javafml.
func loaderCompatibilityVersions(
	module string,
	target Target,
) []versionpkg.Version {
	switch target.Product {
	case serverpkg.ProductNeoForge:
		switch target.Minecraft {
		case "1.19.2":
			switch module {
			case "languageloader.javafml":
				return []versionpkg.Version{"42"}
			case "minecraft":
				return []versionpkg.Version{"1.19.1"}
			case "forge":
				return []versionpkg.Version{"42.0.9"}
			}
		case "1.21.1":
			switch module {
			case "minecraft":
				return []versionpkg.Version{"1.21"}
			case "neoforge":
				return []versionpkg.Version{"21.0.166"}
			}
		case "1.21.8":
			switch module {
			case "minecraft":
				return []versionpkg.Version{"1.21.7"}
			case "neoforge":
				return []versionpkg.Version{"21.7.26-beta"}
			}
		}
	case serverpkg.ProductForge:
		switch target.Minecraft {
		case "1.19.2":
			switch module {
			case "languageloader.javafml":
				return []versionpkg.Version{"42"}
			case "minecraft":
				return []versionpkg.Version{"1.19.1"}
			case "forge":
				return []versionpkg.Version{"42.0.9"}
			}
		case "1.20.1":
			if module == "forge" {
				return []versionpkg.Version{"47.1.79"}
			}
		case "1.21.1":
			switch module {
			case "languageloader.javafml":
				return []versionpkg.Version{"51"}
			case "minecraft":
				return []versionpkg.Version{"1.21"}
			case "forge":
				return []versionpkg.Version{"51.0.33"}
			}
		}
	}
	return nil
}
