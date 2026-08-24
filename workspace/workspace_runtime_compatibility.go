package workspace

import "github.com/mclucy/lucy/types"

// EvaluateRuntimeCompatibility reports whether the detected server supports a
// package ecosystem, and which offer provides that support.
func EvaluateRuntimeCompatibility(
	server *ServerInstance,
	required types.Ecosystem,
) (types.Compatibility, types.Ecosystem) {
	if server == nil || !server.IsValid() {
		return types.CompatUnknown, types.EcoUnspecified
	}
	if required == types.EcoUnspecified {
		return types.CompatCompatible, types.EcoUnspecified
	}

	offers := server.EffectiveEcosystems()
	for _, offer := range offers {
		if offer.Compatibility == types.CompatCompatible &&
			offer.Ecosystem.Satisfy(required) {
			return types.CompatCompatible, offer.Ecosystem
		}
	}
	for _, offer := range offers {
		if offer.Compatibility == types.CompatDegraded &&
			offer.Ecosystem.Satisfy(required) {
			return types.CompatDegraded, offer.Ecosystem
		}
	}
	return types.CompatIncompatible, types.EcoUnspecified
}
