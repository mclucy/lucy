package main

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// These types describe the CLI wire format, not workspace implementation types.
// Keeping the runner independent makes it verify the binary's public output.
type statusOutput struct {
	Server struct {
		PrimaryRuntime    runtimeRef   `json:"primary_runtime"`
		RuntimeComponents []runtimeRef `json:"runtime_components"`
	} `json:"server"`
	ModPath []string `json:"mod_path"`
}

type runtimeRef struct {
	Eco     string `json:"Eco"`
	Name    string `json:"Name"`
	Version string `json:"Version"`
}

func executeStatus(binary, dir string) (statusOutput, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return statusOutput{}, fmt.Errorf("sandbox unavailable (%s): %w", dir, err)
	}
	if !info.IsDir() {
		return statusOutput{}, fmt.Errorf("sandbox is not a directory: %s", dir)
	}
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
