package types

import "strings"

// Source identifies an upstream catalog where package metadata and artifacts can
// be fetched.
//
// Source is a stable semantic identifier used by CLI/config/storage. Execution
// capabilities are implemented by upstream.Provider.
type Source uint8

const (
	SourceAuto Source = iota
	SourceCurseForge
	SourceModrinth
	SourceGitHub
	SourceMCDR
	SourceUnknown
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
