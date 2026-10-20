package types

import "strings"

// Side identifies where a descriptor module is intended to run.
type Side string

const (
	SideUnknown Side = ""
	SideClient  Side = "client"
	SideServer  Side = "server"
	SideBoth    Side = "both"
)

func ParseSide(s string) Side {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "client":
		return SideClient
	case "server":
		return SideServer
	case "*", "both", "any":
		return SideBoth
	default:
		return SideUnknown
	}
}
