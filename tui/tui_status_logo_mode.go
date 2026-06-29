package tui

import "strings"

// StatusLogoMode selects how the status command shows the ASCII platform logo.
// Large is opt-in via --logo=large; default is small.
type StatusLogoMode int

const (
	// StatusLogoSmall is the default: compact logo when layout allows.
	StatusLogoSmall StatusLogoMode = iota
	// StatusLogoNone omits the logo.
	StatusLogoNone
	// StatusLogoLarge uses the full-size logo (side-by-side or stacked).
	StatusLogoLarge
)

// ParseStatusLogoMode parses a --logo flag value (case-insensitive).
// Empty string means small (default).
func ParseStatusLogoMode(s string) (StatusLogoMode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "small":
		return StatusLogoSmall, true
	case "none":
		return StatusLogoNone, true
	case "large":
		return StatusLogoLarge, true
	default:
		return StatusLogoSmall, false
	}
}
