//go:build !release

package telemetry

// defaultEnabled keeps development builds silent until the user enables
// telemetry explicitly.
const defaultEnabled = false
