package types

import "strings"

// Source identifies an upstream catalog where package metadata and artifacts can
// be fetched.
//
// Source is a stable semantic identifier used by CLI/config/storage. It is not
// an execution object.
//   - In user input, Source can express either a concrete upstream
//     (SourceModrinth) or a routing policy marker (SourceAuto).
//   - In result payloads, Source records where data came from.
//   - In routing, Source is the key that resolves to one or more providers.
//
// Execution of native upstream APIs is implemented by upstream.Provider.
type Source uint8

const (
	SourceAuto Source = iota // policy marker: let routing choose providers
	SourceCurseForge
	SourceModrinth
	SourceGitHub
	SourceMCDR
	SourceUnknown // sentinel for parse/validation failure
)

func (s Source) String() string {
	switch s {
	case SourceCurseForge:
		return "curseforge"
	case SourceModrinth:
		return "modrinth"
	case SourceGitHub:
		return "github"
	case SourceMCDR:
		return "mcdr"
	default:
		return "unknown"
	}
}

func (s Source) Title() string {
	switch s {
	case SourceCurseForge:
		return "CurseForge"
	case SourceModrinth:
		return "Modrinth"
	case SourceGitHub:
		return "GitHub"
	case SourceMCDR:
		return "MCDR"
	default:
		return "Unknown"
	}
}

var sourceByString = map[string]Source{
	"auto":       SourceAuto,
	"":           SourceAuto,
	"curseforge": SourceCurseForge,
	"modrinth":   SourceModrinth,
	"github":     SourceGitHub,
	"mcdr":       SourceMCDR,
	"unknown":    SourceUnknown,
}

func ParseSource(s string) Source {
	s = strings.ToLower(s)
	if v, ok := sourceByString[s]; ok {
		return v
	}
	return SourceUnknown
}
