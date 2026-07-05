package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mclucy/lucy/state"
)

type RuntimeState struct {
	PendingRestart bool   `json:"pending_restart"`
	UpdatedAt      string `json:"updated_at"`
	Reason         string `json:"reason,omitempty"`
}

func ReadRuntimeState(name string) (RuntimeState, error) {
	data, ok, err := state.SafeRead(RuntimeStatePath(name))
	if err != nil || !ok {
		return RuntimeState{}, err
	}
	var st RuntimeState
	if err := json.Unmarshal(data, &st); err != nil {
		return RuntimeState{}, fmt.Errorf("parse runtime state %q: %w", name, err)
	}
	return st, nil
}

func MarkPendingRestart(name string, pending bool, reason string) error {
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
	if err := os.MkdirAll(filepath.Dir(RuntimeStatePath(name)), 0o755); err != nil {
		return fmt.Errorf("create runtime state directory: %w", err)
	}
	return state.AtomicWrite(RuntimeStatePath(name), data, 0o644)
}
