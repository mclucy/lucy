package types

// Compatibility is how well a runtime serves an ecosystem:
//
//   - CompatFull: the runtime serves the ecosystem directly.
//   - CompatDegraded: an indirect path, such as a bridge package, serves the
//     ecosystem. One package may fail under this level. This should come with
//     a validator function
type Compatibility string

const (
	CompatFull     Compatibility = "full"
	CompatDegraded Compatibility = "degraded"
)
