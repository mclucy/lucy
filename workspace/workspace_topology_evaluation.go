package workspace

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

func EvaluateCompatibility(
	server *ServerInstance,
	required types.Ecosystem,
) types.CompatResult {
	if server == nil || !server.IsValid() {
		return types.CompatResult{
			Verdict: types.CompatUnresolved,
			Reason:  "server_unresolved",
			Detail:  "Server runtime has not been probed or could not be determined.",
		}
	}

	if required == types.EcoUnspecified {
		return types.CompatResult{
			Verdict: types.CompatCompatible,
			Reason:  "no_ecosystem_requirement",
			Detail:  "Package does not require a specific server ecosystem.",
		}
	}

	for _, offered := range server.PrimaryEcosystem() {
		if offered.Satisfy(required) {
			return types.CompatResult{
				Verdict: types.CompatCompatible,
				Reason:  "direct_ecosystem_match",
				Detail: fmt.Sprintf(
					"Runtime directly supports %s via %s.",
					required,
					offered,
				),
			}
		}
	}

	for _, offered := range server.SecondaryEcosystem() {
		if offered.Satisfy(required) {
			return types.CompatResult{
				Verdict: types.CompatDegraded,
				Reason:  "hosted_ecosystem_match",
				Detail: fmt.Sprintf(
					"Support for %s is available through a hosted or indirect runtime path via %s.",
					required,
					offered,
				),
			}
		}
	}

	return types.CompatResult{
		Verdict: types.CompatIncompatible,
		Reason:  "no_ecosystem_match",
		Detail: fmt.Sprintf(
			"Runtime does not support %s.",
			required,
		),
	}
}
