package install

import (
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

// InstalledConstraint represents a currently-installed package treated as a
// fixed constraint during recursive solving. The package must not be replaced
// automatically; it only contributes as a fixed version anchor.
type InstalledConstraint struct {
	// Package is the installed package with its local installation path.
	Package types.Package

	// ConstraintInput is the fixed constraint edge derived from this installed
	// package, used as an immutable lower-bound in the constraint engine.
	ConstraintInput resolve.ConstraintInput
}

// CandidateNode is a package that has been admitted into the candidate graph.
// A node may be advisory (from upstream APIs) or verified (from local JARs).
// Advisory nodes MUST NOT trigger file-system mutations.
type CandidateNode struct {
	Package      types.ResolvedPackage
	Path         string
	Dependencies *types.PackageDependencies

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
// the install plan towards a stable validated closure.
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
	Tightened []resolve.ConstraintInput
}

// IsStable returns true when the diff has no pending changes, indicating the
// closure has converged to a stable validated graph.
func (d ReconcileDiff) IsStable() bool {
	return len(d.Missing) == 0 && len(d.Extra) == 0 && len(d.Tightened) == 0
}

// ApplyPlan is the final, immutable set of operations to execute. It is
// constructed only after reconcile has produced a stable validated closure.
type ApplyPlan struct {
	Resolved             ResolvedClosure
	InstalledConstraints []InstalledConstraint

	// Install is the ordered list of packages to install.
	Install []types.InstalledPackage

	// Remove is the list of locally-installed packages proven unreachable from
	// the validated closure. Only packages within this transaction's scope are
	// eligible for removal.
	Remove []types.InstalledPackage

	Provenance map[string][]string
}

// ResolvedClosure is the output of the resolve stage.
type ResolvedClosure struct {
	Roots                []types.VersionedPackageRef
	CandidateGraph       map[string]CandidateNode
	InstalledConstraints []InstalledConstraint
	Providers            []upstream.PackageSource
	StagingDir           string
	Ambient              AmbientDependencies
}

// DownloadedClosure is the output of the download stage.
type DownloadedClosure struct {
	Resolved            ResolvedClosure
	DownloadedArtifacts map[string]string
}

type downloadedPackage struct {
	Package types.ResolvedPackage
	Path    string
}

// VerifiedClosure is the output of the verify stage.
type VerifiedClosure struct {
	Downloaded    DownloadedClosure
	VerifiedGraph map[string]CandidateNode
	ReconcileDiff ReconcileDiff
}

func versionedResolvedID(pkg types.ResolvedPackage) types.VersionedPackageRef {
	return types.VersionedPackageRef{
		PackageRef: pkg.Id.PackageRef,
		Version:    pkg.Id.Version,
	}
}

func resolvedPackageLabel(pkg types.ResolvedPackage) string {
	return versionedResolvedID(pkg).StringFull()
}
