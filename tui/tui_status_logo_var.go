package tui

import (
	_ "embed"

	"github.com/mclucy/lucy/types"
)

var (

	//go:embed assets/large_plain/fabric.txt
	fabricNoColorLarge string

	//go:embed assets/small_plain/fabric.txt
	fabricNoColorSmall string

	//go:embed assets/large_plain/forge.txt
	forgeNoColorLarge string

	//go:embed assets/small_plain/forge.txt
	forgeNoColorSmall string

	//go:embed assets/large_plain/neoforge.txt
	neoforgeNoColorLarge string

	//go:embed assets/small_plain/neoforge.txt
	neoforgeNoColorSmall string
)

func GetLogo(platform types.Platform, variant LogoVariant) string {
	switch platform {
	case types.PlatformFabric:
		switch variant {
		case LogoLargePlain:
			return fabricNoColorLarge
		case LogoSmallPlain:
			return fabricNoColorSmall
		default:
			return ""
		}
	case types.PlatformForge:
		switch variant {
		case LogoLargePlain:
			return forgeNoColorLarge
		case LogoSmallPlain:
			return forgeNoColorSmall
		default:
			return ""
		}
	case types.PlatformNeoforge:
		switch variant {
		case LogoLargePlain:
			return neoforgeNoColorLarge
		case LogoSmallPlain:
			return neoforgeNoColorSmall
		default:
			return ""
		}
	default:
		return ""
	}
}
