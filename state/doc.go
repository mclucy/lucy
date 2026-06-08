// Package state defines Lucy's persistent state contracts.
//
// Lucy separates three kinds of state and keeps them in different places on
// purpose:
//
//   - desired state: what the project wants Lucy to manage next
//   - resolved state: the exact dependency closure Lucy selected for that intent
//   - observed state: what probe discovers from the working directory right now
//
// The persistent files for v1 are:
//
//   - lucy.yaml for manifest (desired environment intent) and optional config overrides
//   - lucy-lock.yaml for exact resolved graph, artifact identity, and provenance
//
// Integration contract:
//
// Commands should access persistent state through a project-scoped service that
// loads, validates, saves, reloads, and invalidates state for one working
// directory at a time. Persistent state must not be hidden behind package-level
// mutable singletons. Probe remains the authority for observed state, while the
// install pipeline remains the authority for in-memory reconcile/apply state.
package state
