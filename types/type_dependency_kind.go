package types

import "strings"

// DependencyKind is the relationship declared by a descriptor or provider.
type DependencyKind string

const (
	DependencyKindUnknown      DependencyKind = ""
	DependencyKindRequired     DependencyKind = "required"
	DependencyKindOptional     DependencyKind = "optional"
	DependencyKindRecommended  DependencyKind = "recommended"
	DependencyKindSuggested    DependencyKind = "suggested"
	DependencyKindIncompatible DependencyKind = "incompatible"
	DependencyKindConflict     DependencyKind = "conflict"
	DependencyKindPython       DependencyKind = "python"
)

func ParseDependencyKind(s string) DependencyKind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "required", "mandatory":
		return DependencyKindRequired
	case "optional", "recommends", "recommended":
		return DependencyKindOptional
	case "suggested", "suggests":
		return DependencyKindSuggested
	case "incompatible", "breaks":
		return DependencyKindIncompatible
	case "conflict", "conflicts", "discouraged":
		return DependencyKindConflict
	case "python":
		return DependencyKindPython
	default:
		return DependencyKindUnknown
	}
}
