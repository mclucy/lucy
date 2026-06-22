package resolve

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

// FormatVersionConstraint renders a single version sub-expression as the
// operator sign followed by the version value. It is exported so adjacent
// packages can format conflict clauses without re-implementing the operator
// table.
func FormatVersionConstraint(constraint types.VersionSubExpr) string {
	return constraint.Operator.ToSign() + fmt.Sprint(constraint.Value)
}
