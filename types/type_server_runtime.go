package types

import (
	"github.com/mclucy/lucy/internal/fileschema"
)

// WrapperInfo describes non-jar wrappers around a server directory.
type WrapperInfo struct {
	MCDR *MCDREnv `json:"mcdr,omitempty"`
}

type MCDREnv struct {
	Version Version                    `json:"version"`
	Config  *fileschema.FileMcdrConfig `json:"config,omitempty"`
}
