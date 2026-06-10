package install

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
		return "install: constraint conflict"
	}
	return fmt.Sprintf(
		"install: constraint conflict for %s between %q (%s) and %q (%s)",
		e.PackageId.StringBase(),
		e.Left.Requester,
		formatVersionConstraint(e.Left.Constraint),
		e.Right.Requester,
		formatVersionConstraint(e.Right.Constraint),
	)
}
