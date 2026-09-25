package types

// Compatibility describes how directly a runtime serves an ecosystem.
// CompatFull serves it directly. CompatDegraded serves it through an indirect
// path, so consumers must validate package-specific behavior before relying
// on it.
type Compatibility string

const (
	CompatFull     Compatibility = "full"
	CompatDegraded Compatibility = "degraded"
)
