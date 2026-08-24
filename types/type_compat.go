package types

type Compatibility string

const (
	CompatUnknown      Compatibility = "unknown"
	CompatCompatible   Compatibility = "compatible"
	CompatDegraded     Compatibility = "degraded"
	CompatIncompatible Compatibility = "incompatible"
)
