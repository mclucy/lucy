package types

type CompatVerdict string

const (
	CompatCompatible   CompatVerdict = "compatible"
	CompatDegraded     CompatVerdict = "degraded"
	CompatIncompatible CompatVerdict = "incompatible"
	CompatUnresolved   CompatVerdict = "unresolved"
)

type CompatResult struct {
	Verdict CompatVerdict `json:"verdict"`
	Reason  string        `json:"reason"`
	Detail  string        `json:"detail"`
}
