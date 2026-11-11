package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/artifact"
)

// ErrInstallerUnsupported reports an identified installer whose native method
// this package cannot execute yet.
var ErrInstallerUnsupported = errors.New("installer execution is not supported")

// RunInstaller executes an identified installer with its native entry point
// in root. It requires a Java executable for every supported installer.
func RunInstaller(
	ctx context.Context,
	root string,
	installer artifact.Installer,
	java string,
	output io.Writer,
) error {
	switch installer.Kind {
	case artifact.InstallerForge, artifact.InstallerNeoForge:
		if err := requireJava(java, fmt.Sprintf("%s installer", installer.Kind)); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, java, "-jar", installer.FilePath, "--installServer")
		cmd.Dir = root
		captured, err := runLockedProcess(cmd, output)
		if err != nil {
			return fmt.Errorf("%s: %w\n%s", installer.Kind, err, captured)
		}
		return nil
	case artifact.InstallerFabric, artifact.InstallerQuilt:
		return fmt.Errorf("%w: %s", ErrInstallerUnsupported, installer.Kind)
	default:
		return ErrInstallerUnsupported
	}
}

// InstallerJavaAdvisory returns the Java major version an installer for the
// given Minecraft version needs. It returns 0 when the version is unknown.
func InstallerJavaAdvisory(installer artifact.Installer) int {
	major, minor, patch, ok := parseMinecraftRelease(string(installer.Minecraft))
	if !ok {
		return 0
	}
	switch {
	case major == 1 && (minor > 20 || minor == 20 && patch >= 5):
		return 21
	case major == 1 && minor >= 18:
		return 17
	case major == 1 && minor >= 17:
		return 16
	default:
		return 8
	}
}

// parseMinecraftRelease splits a Minecraft release version into numeric
// components. It requires one of the forms 1.N or 1.N.P. Any other input,
// including snapshots and prereleases, is unknown.
func parseMinecraftRelease(raw string) (major, minor, patch int, ok bool) {
	parts := strings.Split(raw, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, 0, 0, false
	}
	values := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return 0, 0, 0, false
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return 0, 0, 0, false
		}
		values[i] = value
	}
	if values[0] != 1 {
		return 0, 0, 0, false
	}
	if len(values) == 2 {
		return values[0], values[1], 0, true
	}
	return values[0], values[1], values[2], true
}
