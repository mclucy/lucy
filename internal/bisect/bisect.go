// Package bisect persists and advances a binary-search session over the
// enabled requirements of a manifest. It owns the session lock, state
// serialization, manifest transitions, installation sync, and recovery.
// Command wiring and rendering stay at the command boundary.
package bisect

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/depgraph"
	"github.com/mclucy/lucy/server"
)

// Mod is one manifest requirement the session toggles. Reference is the
// manifest's own "provider:project" key and Ecosystem is the ecosystem it is
// declared under; together they identify the entry unambiguously.
type Mod struct {
	Environment server.Environment `json:"environment"`
	Ecosystem   catalog.Ecosystem  `json:"ecosystem"`
	Reference   string             `json:"reference"`
	Version     string             `json:"version,omitempty"`
}

// Label renders the requirement reference with its recorded version.
func (m Mod) Label() string {
	if m.Version == "" {
		return m.Reference
	}
	return m.Reference + "@" + m.Version
}

// Phase is the durable phase of a session. A session starts active and ends
// in exactly one terminal phase; only reset returns the workspace to having
// no session.
type Phase string

const (
	PhaseActive    Phase = "active"
	PhaseFound     Phase = "found"
	PhaseExhausted Phase = "exhausted"
)

// Step names the leaf that owns a pending transition. A retry must run the
// same leaf, so the step is recorded instead of inferred from the range.
type Step string

const (
	StepStart Step = "start"
	StepGood  Step = "good"
	StepBad   Step = "bad"
	StepReset Step = "reset"
)

// View is the search position a command reports.
type View struct {
	Total     int  `json:"total"`
	Left      int  `json:"left"`
	Right     int  `json:"right"`
	Midpoint  int  `json:"midpoint,omitempty"`
	Candidate *Mod `json:"candidate,omitempty"`
	Remaining int  `json:"remaining"`
}

// Result is the observable outcome of one session command. Message text and
// document rendering belong to the caller.
type Result struct {
	Phase    Phase
	Pending  Step
	Complete bool
	Found    *Mod
	// Marked is the requirement a completed good or bad transition marked.
	Marked *Mod
	State  *View

	Enabled  int
	Disabled int
	Restored int
	Warnings []string

	// Findings is the live graph observation candidate selection consumed.
	Findings depgraph.Findings
	// NoCandidates reports a start that found no enabled requirement.
	NoCandidates bool
}
