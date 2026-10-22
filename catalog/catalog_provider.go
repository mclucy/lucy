package catalog

import "strings"

// Provider identifies the execution layer that fetches metadata or artifacts.
// A provider may or may not back a Registry.
type Provider string

const (
	ProviderUnspecified Provider = ""
	ProviderUnknown     Provider = "unknown"

	ProviderModrinth   Provider = "modrinth"
	ProviderCurseForge Provider = "curseforge"
	ProviderHangar     Provider = "hangar"
	ProviderSpiget     Provider = "spiget"
	ProviderMCDR       Provider = "mcdr"

	ProviderGitHub   Provider = "github"
	ProviderMojang   Provider = "mojang"
	ProviderFabric   Provider = "fabric"
	ProviderForge    Provider = "forge"
	ProviderNeoForge Provider = "neoforge"
	ProviderBMCLAPI  Provider = "bmclapi"
	ProviderPaperMC  Provider = "papermc"
	ProviderPurpur   Provider = "purpur"
	ProviderMaven    Provider = "maven"
	ProviderPyPI     Provider = "pypi"
)

// String returns the persisted provider key. The unspecified provider is
// rendered as "any" for display only; use the value itself for storage.
func (p Provider) String() string {
	if p == ProviderUnspecified {
		return "any"
	}
	return string(p)
}

// Title returns the human-facing provider name.
func (p Provider) Title() string {
	switch p {
	case ProviderModrinth:
		return "Modrinth"
	case ProviderCurseForge:
		return "CurseForge"
	case ProviderHangar:
		return "Hangar"
	case ProviderSpiget:
		return "Spiget"
	case ProviderMCDR:
		return "MCDR"
	case ProviderGitHub:
		return "GitHub"
	case ProviderMojang:
		return "Mojang"
	case ProviderFabric:
		return "Fabric"
	case ProviderForge:
		return "Forge"
	case ProviderNeoForge:
		return "NeoForge"
	case ProviderBMCLAPI:
		return "BMCLAPI"
	case ProviderPaperMC:
		return "PaperMC"
	case ProviderPurpur:
		return "Purpur"
	case ProviderMaven:
		return "Maven"
	case ProviderPyPI:
		return "PyPI"
	case ProviderUnspecified:
		return "Any"
	default:
		return "Unknown"
	}
}

// Registry returns the registry backed by p. Providers such as GitHub, Maven,
// and PyPI are execution layers without a registry.
func (p Provider) Registry() (Registry, bool) {
	switch p {
	case ProviderModrinth:
		return RegistryModrinth, true
	case ProviderCurseForge:
		return RegistryCurseForge, true
	case ProviderHangar:
		return RegistryHangar, true
	case ProviderSpiget:
		return RegistrySpiget, true
	case ProviderMCDR:
		return RegistryMCDR, true
	default:
		return RegistryUnspecified, false
	}
}

// ParseProvider maps a provider key to its typed value. The empty key means
// the unspecified provider, which lets routing choose.
func ParseProvider(s string) (Provider, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ProviderUnspecified, true
	}
	switch Provider(s) {
	case ProviderUnknown,
		ProviderModrinth,
		ProviderCurseForge,
		ProviderHangar,
		ProviderSpiget,
		ProviderMCDR,
		ProviderGitHub,
		ProviderMojang,
		ProviderFabric,
		ProviderForge,
		ProviderNeoForge,
		ProviderBMCLAPI,
		ProviderPaperMC,
		ProviderPurpur,
		ProviderMaven,
		ProviderPyPI:
		return Provider(s), true
	default:
		return ProviderUnknown, false
	}
}
