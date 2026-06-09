package types

import "github.com/mclucy/lucy/exttype"

type ServerActivity struct {
	Active bool `json:"active"`
	Pid    int  `json:"pid"`
}

type EnvironmentInfo struct {
	Lucy *LucyEnv `json:"lucy,omitempty"`
	Mcdr *McdrEnv `json:"mcdr,omitempty"`
}

type McdrEnv struct {
	Version BareVersion             `json:"version"`
	Config  *exttype.FileMcdrConfig `json:"config,omitempty"`
}

// LucyEnv is a placeholder for Lucy environment; currently just a boolean
// indicating presence, but can be expanded with more details if needed
type LucyEnv bool
