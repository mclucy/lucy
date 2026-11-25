package cmd

import (
	"os"
	"testing"
)

// TestMain keeps the suite silent. These tests run command trees in process, so
// a telemetry-enabled binary would report every command they exercise.
func TestMain(m *testing.M) {
	_ = os.Setenv("DO_NOT_TRACK", "1")
	os.Exit(m.Run())
}
