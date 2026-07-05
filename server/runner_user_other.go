//go:build !unix && !darwin && !linux

package server

import "os/exec"

func applyRunUser(_ *exec.Cmd, _ string) error {
	return nil
}
