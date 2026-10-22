package catalog

import "strings"

// Registry identifies a package repository with search and package metadata.
// A registry is executed by a Provider.
type Registry string

const (
	RegistryUnspecified Registry = ""
	RegistryUnknown     Registry = "unknown"

	RegistryModrinth   Registry = "modrinth"
	RegistryCurseForge Registry = "curseforge"
	RegistryHangar     Registry = "hangar"
	RegistrySpiget     Registry = "spiget"
	RegistryMCDR       Registry = "mcdr"
)

func (r Registry) String() string {
	if r == RegistryUnspecified {
		return "any"
	}
	return string(r)
}

func (r Registry) Title() string {
	switch r {
	case RegistryModrinth:
		return "Modrinth"
	case RegistryCurseForge:
		return "CurseForge"
	case RegistryHangar:
		return "Hangar"
	case RegistrySpiget:
		return "Spiget"
	case RegistryMCDR:
		return "MCDR"
	case RegistryUnspecified:
		return "Any"
	default:
		return "Unknown"
	}
}

// Provider returns the execution layer that handles r.
func (r Registry) Provider() Provider {
	switch r {
	case RegistryModrinth:
		return ProviderModrinth
	case RegistryCurseForge:
		return ProviderCurseForge
	case RegistryHangar:
		return ProviderHangar
	case RegistrySpiget:
		return ProviderSpiget
	case RegistryMCDR:
		return ProviderMCDR
	default:
		return ProviderUnknown
	}
}

// ParseRegistry maps a registry key to its typed value. The empty key means
// all registries.
func ParseRegistry(s string) (Registry, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return RegistryUnspecified, true
	}
	switch Registry(s) {
	case RegistryUnknown,
		RegistryModrinth,
		RegistryCurseForge,
		RegistryHangar,
		RegistrySpiget,
		RegistryMCDR:
		return Registry(s), true
	default:
		return RegistryUnknown, false
	}
}
