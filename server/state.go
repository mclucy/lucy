package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mclucy/lucy/state"
)

// RuntimeState records transient supervisor metadata under the daemon state
// directory. It is independent of ProjectStateService's project manifest and
// reproducible lockfile; a restart marker must not alter either project document.
type RuntimeState struct {
	PendingRestart bool   `json:"pending_restart"`
	UpdatedAt      string `json:"updated_at"`
	Reason         string `json:"reason,omitempty"`
}

// RuntimeStateService owns per-instance supervisor state in a fixed directory.
// It does not load or modify a server's project manifest or lockfile.
type RuntimeStateService struct {
	dir string
}

// NewRuntimeStateService captures the configured daemon state directory without
// creating it; later environment changes do not redirect this service's writes.
func NewRuntimeStateService() *RuntimeStateService {
	return &RuntimeStateService{dir: RuntimeStateDir()}
}

// Read validates name and loads its restart marker, returning an empty state
// when no marker exists and an error for unreadable or malformed JSON.
func (s *RuntimeStateService) Read(name string) (RuntimeState, error) {
	if err := ValidateInstanceName(name); err != nil {
		return RuntimeState{}, err
	}
	data, ok, err := state.SafeRead(filepath.Join(s.dir, name+".json"))
	if err != nil || !ok {
		return RuntimeState{}, err
	}
	var st RuntimeState
	if err := json.Unmarshal(data, &st); err != nil {
		return RuntimeState{}, fmt.Errorf("parse runtime state %q: %w", name, err)
	}
	return st, nil
}

// MarkPendingRestart validates name and atomically records its restart flag,
// reason and current UTC timestamp, creating the state directory if needed.
func (s *RuntimeStateService) MarkPendingRestart(name string, pending bool, reason string) error {
	if err := ValidateInstanceName(name); err != nil {
		return err
	}
	st := RuntimeState{
		PendingRestart: pending,
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
		Reason:         reason,
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize runtime state: %w", err)
	}
	path := filepath.Join(s.dir, name+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create runtime state directory: %w", err)
	}
	return state.AtomicWrite(path, data, 0o644)
}
