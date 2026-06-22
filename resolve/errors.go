package resolve

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

// ConstraintConflictSource identifies one requester-side clause participating in
// an irreconcilable merged constraint.
type ConstraintConflictSource struct {
	Requester  string
	Constraint types.VersionSubExpr
}

// ConstraintConflictError reports that merged requirements for one package
// identity have no satisfiable intersection.
type ConstraintConflictError struct {
	PackageId types.VersionedPackageRef
	Left      ConstraintConflictSource
	Right     ConstraintConflictSource
}

func (e *ConstraintConflictError) Error() string {
	if e == nil {
		return "resolve: constraint conflict"
	}
	return fmt.Sprintf(
		"resolve: constraint conflict for %s between %q (%s) and %q (%s)",
		e.PackageId.StringBase(),
		e.Left.Requester,
		FormatVersionConstraint(e.Left.Constraint),
		e.Right.Requester,
		FormatVersionConstraint(e.Right.Constraint),
	)
}
