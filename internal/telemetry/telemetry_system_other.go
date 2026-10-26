//go:build !windows && !darwin && !linux

package telemetry

func osVersion() string {
	return ""
}
