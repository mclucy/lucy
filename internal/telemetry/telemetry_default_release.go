//go:build release

package telemetry

// defaultEnabled follows the Alpha and Beta distribution policy. Explicit
// configuration always wins.
const defaultEnabled = true
