package cmd

import (
	"os"
	"testing"
)

// TestMain keeps the suite silent. These tests run command trees in process, so
// a telemetry-enabled binary would report every command they exercise.
func TestMain(m *testing.M) {
	_ = os.Setenv("LUCY_TELEMETRY", "off")
	os.Exit(m.Run())
}
