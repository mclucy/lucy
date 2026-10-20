//go:build e2e

package status

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os/exec"
	"strings"
)

// These types describe the CLI wire format, not workspace implementation types.
// Keeping the runner independent makes it verify the binary's public output.
type statusOutput struct {
	Server struct {
		Primary    runtimeRef   `json:"primary_runtime"`
		Components []runtimeRef `json:"runtime_components"`
	} `json:"server"`
	PackageDirs []string `json:"package_dirs"`
}

type runtimeRef struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Ecosystem string `json:"ecosystem"`
	Version   string `json:"version"`
}

func executeStatus(binary, dir string) (statusOutput, error) {
	cmd := exec.Command(binary, "status", "--json", "--no-style")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return statusOutput{}, fmt.Errorf("lucy status: %w; stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	var status statusOutput
	if err := json.Unmarshal(stdout, &status); err != nil {
		return statusOutput{}, fmt.Errorf("decode lucy status JSON: %w; stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return status, nil
}
