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
//   - .lucy/config.toml for policy, defaults, and operator-selected behavior
//   - .lucy/manifest.toml for desired environment intent
//   - .lucy/lock.json for exact resolved graph, artifact identity, and provenance
//
// Ownership rules are strict.
//
// Config owns policy and defaults. It may describe source preference, safety
// policy, output defaults, and other knobs that affect future commands, but it
// must not declare desired packages, exact artifact hashes, download URLs, or
// live probe facts.
//
// Manifest owns intent. It may describe what environment Lucy should converge
// toward, which packages are direct roots, and which managed scope boundaries
// are explicitly under Lucy control. It must stay descriptive rather than
// procedural: no exact resolved artifact metadata, no transient planner state,
// and no runtime observations belong here.
//
// Lock owns the resolved closure. It records the exact graph selected for a
// manifest plus the provenance needed to explain how that closure was produced.
// It may contain exact versions, source selections, hashes, download locations,
// and resolution lineage, but it must not become a dump of probe.ServerInfo or
// an execution plan for long-running orchestration.
//
// Observed state is not persisted by this package in v1. Live runtime,
// topology, world activity, player data, and other environmental facts stay in
// probe.ServerInfo and related probe outputs. Commands must treat observed
// state as fresh input, not as something copied into config, manifest, or lock.
//
// Cross-boundary rules:
//
//   - manifest must not absorb lockfile-only fields such as hashes, exact URLs,
//     or fully expanded transitive closures
//   - lock must not claim ownership of user policy or desired roots
//   - config must not silently override manifest intent or replace lock
//     provenance
//   - none of the persistent files may store volatile world/player/runtime live
//     state in v1
//
// Integration contract:
//
// Commands should access persistent state through a project-scoped service that
// loads, validates, saves, reloads, and invalidates state for one working
// directory at a time. Persistent state must not be hidden behind package-level
// mutable singletons. Probe remains the authority for observed state, while the
// install pipeline remains the authority for in-memory reconcile/apply state.
package state
