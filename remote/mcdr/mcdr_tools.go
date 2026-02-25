package mcdr

import (
	"strings"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/types"
)

func parseRequiredVersion(s string) (reqs []types.VersionConstraint) {
	split := strings.Split(s, ",")
	for _, expr := range split {
		expr = strings.TrimSpace(expr)

		if expr == "*" {
			// No specific requirement
			continue
		}

		version := strings.TrimLeft(expr, "<>=!^~")
		req := types.VersionConstraint{
			Value: dependency.Parse(
				types.RawVersion(version),
				types.Semver,
			),
		}

		// Currently, I did not see the x.x.x or *.*.* pattern in MCDR's plugin
		// requirements, so I will not implement it for now.
		if strings.HasPrefix(expr, "=") {
			req.Operator = types.OpEq
		} else if strings.HasPrefix(expr, "~") ||
			strings.HasPrefix(expr, "~=") {
			req.Operator = types.OpWeakEq
		} else if strings.HasPrefix(expr, "<=") {
			req.Operator = types.OpLte
		} else if strings.HasPrefix(expr, "<") {
			req.Operator = types.OpLt
		} else if strings.HasPrefix(expr, ">=") {
			req.Operator = types.OpGte
		} else if strings.HasPrefix(expr, ">") {
			req.Operator = types.OpGt
		} else if strings.HasPrefix(expr, "!=") {
			req.Operator = types.OpNeq
		} else if strings.HasPrefix(expr, "^") {
			req.Operator = types.OpWeakGt
		} else {
			req.Operator = types.OpEq
		}

		reqs = append(reqs, req)
	}
	return
}
