//go:build e2e

package status

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// These types describe the CLI wire format, not workspace implementation types.
// Keeping the runner independent makes it verify the binary's public output.
type statusOutput struct {
	Server struct {
		Product            runtimeRef   `json:"product"`
		Components         []runtimeRef `json:"components"`
		PackageDirectories []string     `json:"package_directories"`
	} `json:"server"`
	Packages []runtimeRef `json:"packages"`
}

func (p runtimeRef) identifier() string {
	return p.Ecosystem + ":" + p.Name + "@" + p.Version
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
	cmd.Env = slices.DeleteFunc(os.Environ(), func(value string) bool {
		return strings.HasPrefix(value, "LUCY_TELEMETRY=")
	})
	cmd.Env = append(cmd.Env, "LUCY_TELEMETRY=off")
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
