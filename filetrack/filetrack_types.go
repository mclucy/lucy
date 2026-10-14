// Package filetrack manages accepted workspace file revisions over independent CAS storage.
package filetrack

import (
	"time"

	"github.com/mclucy/lucy/types"
)

type Operation string

const (
	Track   Operation = "track"
	Untrack Operation = "untrack"
	Update  Operation = "update"
	Recover Operation = "recover"
	Revert  Operation = "revert"
	Lock    Operation = "lock"
	Unlock  Operation = "unlock"
)

// Request contains explicit user choices for one recoverable workspace operation.
type Request struct {
	Operation  Operation
	Path       string
	All        bool
	FromSource bool
	Immutable  bool
	NoHash     bool
	RecoverURL string
	Hash       string
	Revision   uint64
}

// Change separates displaced bytes from accepted revision navigation.
type Change struct {
	Path       string             `json:"path"`
	Before     *types.TrackedFile `json:"before,omitempty"`
	After      *types.TrackedFile `json:"after,omitempty"`
	BackupHash string             `json:"backup_hash,omitempty"`
	BackupMode uint32             `json:"backup_mode,omitzero"`
	Revision   uint64             `json:"revision,omitzero"`
	Parent     uint64             `json:"parent,omitzero"`
	Accepted   bool               `json:"accepted,omitzero"`
	Mode       uint32             `json:"mode,omitzero"`
}

// Event is the commit receipt for a transaction. Sequence order is independent of wall clocks.
type Event struct {
	Sequence  uint64    `json:"sequence"`
	Time      time.Time `json:"time"`
	ID        string    `json:"id"`
	Operation Operation `json:"operation"`
	Changes   []Change  `json:"changes"`
}

type Result struct {
	Event   Event    `json:"event"`
	Skipped []string `json:"skipped,omitempty"`
}

// Status contains live observations. It is never persisted in a manifest or lockfile.
type Status struct {
	Path       string `json:"path"`
	Immutable  bool   `json:"immutable,omitzero"`
	Hash       string `json:"hash,omitempty"`
	ActualHash string `json:"actual_hash,omitempty"`
	State      string `json:"state"`
	RecoverURL string `json:"recover_url,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Diagnostic is an actionable best-effort installation finding.
type Diagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}
