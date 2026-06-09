package types

import "strings"

// SourceId identifies an upstream catalog where package metadata and artifacts can
// be fetched.
//
// SourceId is a stable semantic identifier used by CLI/config/storage. It is not
// an execution object.
//   - In user input, SourceId can express either a concrete upstream
//     (SourceModrinth) or a routing policy marker (SourceAuto).
//   - In result payloads, SourceId records where data came from.
//   - In routing, SourceId is the key that resolves to one or more providers.
//
// Execution of native upstream APIs is implemented by upstream.Provider.
type SourceId uint8

const (
	SourceAuto SourceId = iota // policy marker: let routing choose providers
	SourceCurseForge
	SourceModrinth
	SourceGitHub
	SourceMCDR
	SourceHangar
	SourceSpiget
	SourceUnknown // sentinel for parse/validation failure
)

func (s SourceId) String() string {
	switch s {
	case SourceCurseForge:
		return "curseforge"
	case SourceModrinth:
		return "modrinth"
	case SourceGitHub:
		return "github"
	case SourceMCDR:
		return "mcdr"
	case SourceHangar:
		return "hangar"
	case SourceSpiget:
		return "spiget"
	default:
		return "unknown"
	}
}

func (s SourceId) Title() string {
	switch s {
	case SourceCurseForge:
		return "CurseForge"
	case SourceModrinth:
		return "Modrinth"
	case SourceGitHub:
		return "GitHub"
	case SourceMCDR:
		return "MCDR"
	case SourceHangar:
		return "Hangar"
	case SourceSpiget:
		return "Spiget"
	default:
		return "Unknown"
	}
}

var sourceByString = map[string]SourceId{
	"auto":       SourceAuto,
	"":           SourceAuto,
	"curseforge": SourceCurseForge,
	"modrinth":   SourceModrinth,
	"github":     SourceGitHub,
	"mcdr":       SourceMCDR,
	"hangar":     SourceHangar,
	"spiget":     SourceSpiget,
	"unknown":    SourceUnknown,
}

func ParseSource(s string) SourceId {
	s = strings.ToLower(s)
	if v, ok := sourceByString[s]; ok {
		return v
	}
	return SourceUnknown
}
