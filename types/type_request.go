package types

// PackageRequest expresses one package selection for a target ecosystem.
// EcoUnspecified is resolved from the workspace before provider routing.
type PackageRequest struct {
	PackageRef
	Eco     Ecosystem
	Version BareVersion
}
