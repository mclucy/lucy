package terminal

import "strings"

// StatusLogoMode selects how the status command shows the ASCII platform logo.
// The zero value leaves the mode unset for direct FieldLogo callers.
type StatusLogoMode int

const (
	statusLogoAuto StatusLogoMode = iota
	// StatusLogoSmall selects the compact logo when layout allows.
	StatusLogoSmall
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
