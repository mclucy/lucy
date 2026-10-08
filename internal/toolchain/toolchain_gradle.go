package toolchain

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func probeGradle(ctx context.Context) (*GradleInstallation, error) {
	path, err := exec.LookPath("gradle")
	if err != nil {
		return nil, nil
	}
	if managerShim(path) {
		return nil, fmt.Errorf("Gradle on PATH is a version-manager shim; select an installed distribution")
	}
	output, err := runProbe(ctx, path, "--version")
	if err != nil {
		return nil, fmt.Errorf("probe Gradle %q: %w", path, err)
	}
	for line := range strings.SplitSeq(output, "\n") {
		version, ok := strings.CutPrefix(strings.TrimSpace(line), "Gradle ")
		if ok {
			version, _, _ = strings.Cut(version, " ")
			if version != "" {
				return &GradleInstallation{Path: canonical(path), Version: version}, nil
			}
		}
	}
	return nil, fmt.Errorf("Gradle %q reported no version", path)
}
