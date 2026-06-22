package resolve

import "github.com/mclucy/lucy/types"

// ConstraintInput is a single advisory or installed dependency edge fed into
// the constraint merge engine. It carries the requester identity for conflict
// provenance reporting.
type ConstraintInput struct {
	// Requester is a human-readable label identifying which package or root
	// requested this dependency (e.g. "root", "fabric-api@0.97.2+1.21.1").
	Requester string

	// Dependency is the dependency constraint being asserted by Requester.
	Dependency types.Dependency
}
