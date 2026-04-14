package install

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

func formatVersionConstraint(constraint types.VersionConstraint) string {
	return constraint.Operator.ToSign() + fmt.Sprint(constraint.Value)
}
