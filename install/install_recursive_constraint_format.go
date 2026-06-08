package install

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

func formatVersionConstraint(constraint types.VersionSubExpr) string {
	return constraint.Operator.ToSign() + fmt.Sprint(constraint.Value)
}
