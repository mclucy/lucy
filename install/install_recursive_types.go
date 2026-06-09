package install

import (
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

// RecursivePhase describes the current lifecycle phase of a RecursiveTransaction.
// Phases advance monotonically; no phase may be skipped, and committed state
// is never reachable before verified state has been established.
type RecursivePhase uint8

const (
	// PhaseCandidate is the initial phase. The transaction holds root requests
	// and advisory upstream dependency metadata. No local facts are present yet.
	PhaseCandidate RecursivePhase = iota

	// PhaseDownloaded means all candidate artifacts have been downloaded to a
	// staging area. Advisory upstream edges are still the only dependency source.
	PhaseDownloaded

	// PhaseVerified means downloaded JARs have been analyzed by local detectors
	// and verified dependency facts replace the advisory upstream edges.
	// Conflict detection has run successfully. This is the gate before apply.
	PhaseVerified

	// PhaseCommitted is reached only after a validated, conflict-free closure
	// exists. File-system mutations are allowed only in this phase.
	PhaseCommitted
)

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

// InstalledConstraint represents a currently-installed package treated as a
// fixed constraint during recursive solving. The package must not be replaced
// automatically; it only contributes as a fixed version anchor.
type InstalledConstraint struct {
	// Package is the installed package with its local installation path.
	Package types.Package

	// ConstraintInput is the fixed constraint edge derived from this installed
	// package, used as an immutable lower-bound in the constraint engine.
	ConstraintInput ConstraintInput
}

// CandidateNode is a package that has been admitted into the candidate graph.
// A node may be advisory (from upstream APIs) or verified (from local JARs).
// Advisory nodes MUST NOT trigger file-system mutations.
type CandidateNode struct {
	// Package holds the package metadata. At PhaseCandidate, only Remote may be
	// populated. At PhaseVerified, Local.Path will be set.
	Package types.Package

	// ProvenancePath records the chain of requesters that caused this node to
	// enter the graph, starting from a root request. This is used by conflict
	// reporting and reconcile diff output.
	ProvenancePath []string

	// Advisory is true when this node's dependency facts come from an upstream
	// API. Advisory nodes' Dependencies should NOT be treated as authoritative.
	Advisory bool
}

// ReconcileDiff records the difference between the advisory candidate graph
// and the verified local graph. It drives the reconcile loop that converges
// the transaction towards a stable validated closure.
type ReconcileDiff struct {
	// Missing are packages present in the verified graph but absent from the
	// current candidate graph. They must be added and downloaded before apply.
	Missing []types.VersionedPackageRef

	// Extra are candidate nodes present only in the advisory upstream graph but
	// not reachable from the verified closure. They must be dropped before apply.
	Extra []types.VersionedPackageRef

	// Tightened are packages whose verified constraints are stricter than the
	// advisory upstream constraints. The constraint engine must be re-run with
	// the tighter constraints.
	Tightened []ConstraintInput
}

// IsStable returns true when the diff has no pending changes, indicating the
// transaction has converged to a stable validated closure.
func (d ReconcileDiff) IsStable() bool {
	return len(d.Missing) == 0 && len(d.Extra) == 0 && len(d.Tightened) == 0
}

// ApplyPlan is the final, immutable set of operations to execute during the
// committed phase. It is constructed only after reconcile has produced a stable
// validated closure. No file-system mutations happen before this struct exists.
type ApplyPlan struct {
	// Install is the ordered list of packages to install in this transaction.
	Install []types.Package

	// Remove is the list of locally-installed packages proven unreachable from
	// the validated closure. Only packages within this transaction's scope are
	// eligible for removal.
	Remove []types.Package
}

// RecursiveTransaction is the central state object for a recursive install
// operation. It is passed between all pipeline stages (candidate expansion,
// download, local verification, reconcile, apply) rather than loose slices.
//
// Value boundaries:
//   - PURE fields (no side effects): Phase, Roots, InstalledConstraints,
//     CandidateGraph, VerifiedGraph, ReconcileDiff, Apply
//   - ADAPTER-OWNED fields (side-effect capable): Providers (network I/O),
//     DownloadedArtifacts (filesystem), StagingDir (filesystem paths)
//
// Invariants enforced by this type's construction and phase transitions:
//   - Phase starts at PhaseCandidate; it advances only via AdvanceTo.
//   - ApplyPlan may only be set when Phase == PhaseVerified.
//   - No caller may invoke file-system mutations while Phase < PhaseCommitted.
//   - InstalledConstraints are immutable after transaction construction.
type RecursiveTransaction struct {
	// Phase is the current lifecycle stage. See RecursivePhase constants.
	// PURE: no side effects.
	Phase RecursivePhase

	// Roots are the top-level package IDs requested by the user.
	// PURE: just data.
	Roots []types.VersionedPackageRef

	// InstalledConstraints is a snapshot of currently-installed packages taken
	// from probe.ServerInfo() at transaction start. These are fixed constraints
	// that the solver must respect; they are never auto-replaced.
	// PURE: computed once at transaction creation; no live filesystem/network.
	InstalledConstraints []InstalledConstraint

	// Providers are the upstream provider instances used for dependency fetches
	// during candidate graph expansion.
	// ADAPTER-OWNED: performs network I/O via upstream.Provider interface.
	Providers []upstream.Provider

	// CandidateGraph is the advisory dependency closure computed from upstream
	// APIs and installed constraints. Keyed by PackageId.StringPlatformName().
	// PURE: computed from Provider results (already fetched).
	CandidateGraph map[string]CandidateNode

	// DownloadedArtifacts maps PackageId.StringFull() to the local file path
	// of the downloaded JAR. Populated during PhaseDownloaded.
	// ADAPTER-OWNED: contains filesystem paths.
	DownloadedArtifacts map[string]string

	// VerifiedGraph is the authoritative dependency closure derived from local
	// JAR detector analysis. Populated during PhaseVerified. Supersedes
	// advisory facts in CandidateGraph for conflict and reconcile decisions.
	// PURE: computed from local JAR analysis results.
	VerifiedGraph map[string]CandidateNode

	// ReconcileDiff is the latest diff between candidate and verified graphs.
	// It is updated on each reconcile iteration and must be stable (IsStable())
	// before the transaction may advance to PhaseVerified.
	// PURE: computed from graph comparison; no side effects.
	ReconcileDiff ReconcileDiff

	// Apply holds the finalized operation set. It is set only once, immediately
	// before advancing to PhaseCommitted.
	// PURE: computed plan; actual filesystem mutations happen outside this type.
	Apply *ApplyPlan

	// StagingDir is the temporary directory where artifacts are downloaded
	// during the download phase. Used for atomic move to target directory.
	// ADAPTER-OWNED: filesystem path string.
	StagingDir string
}

// NewRecursiveTransaction constructs a transaction in PhaseCandidate with the
// given root IDs and provider list. The installed constraints snapshot must
// be populated by the caller from probe.ServerInfo() before expansion begins.
func NewRecursiveTransaction(
	roots []types.VersionedPackageRef,
	providers []upstream.Provider,
) *RecursiveTransaction {
	return &RecursiveTransaction{
		Phase:               PhaseCandidate,
		Roots:               roots,
		Providers:           providers,
		CandidateGraph:      make(map[string]CandidateNode),
		DownloadedArtifacts: make(map[string]string),
		VerifiedGraph:       make(map[string]CandidateNode),
	}
}

// AdvanceTo advances the transaction phase to next. It panics if the requested
// phase is not a strict successor of the current phase, enforcing monotonic
// forward-only advancement.
func (tx *RecursiveTransaction) AdvanceTo(next RecursivePhase) {
	if next != tx.Phase+1 {
		panic("install: RecursiveTransaction phase advancement out of order")
	}
	if next == PhaseCommitted && tx.Apply == nil {
		panic("install: cannot commit transaction without a validated ApplyPlan")
	}
	tx.Phase = next
}

// SetApplyPlan finalizes the apply plan. It may only be called when the
// transaction is in PhaseVerified. The transaction must then be advanced to
// PhaseCommitted before mutations begin.
func (tx *RecursiveTransaction) SetApplyPlan(plan ApplyPlan) {
	if tx.Phase != PhaseVerified {
		panic("install: ApplyPlan may only be set in PhaseVerified")
	}
	tx.Apply = &plan
}
