package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/mclucy/lucy/artifact"
	versionpkg "github.com/mclucy/lucy/version"
)

// ErrInstallerUnsupported reports an identified installer whose native method
// this package cannot execute yet.
var ErrInstallerUnsupported = errors.New("installer execution is not supported")

// InstallerSupported reports whether RunInstaller can execute kind natively.
// Callers use it before selecting Java for an installer they cannot run.
func InstallerSupported(kind artifact.InstallerKind) bool {
	switch kind {
	case artifact.InstallerForge, artifact.InstallerNeoForge:
		return true
	default:
		return false
	}
}

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
	default:
		return fmt.Errorf("%w: %s", ErrInstallerUnsupported, installer.Kind)
	}
}

// nativeInstallerJava is the Java major version a Forge-family native
// installer needs. It does not depend on the Minecraft version the installer
// provisions.
const nativeInstallerJava = 17

// InstallerJavaAdvisory returns the Java major version an installer for the
// given Minecraft version needs. A version whose release line is unknown
// answers the native installer's own minimum.
func InstallerJavaAdvisory(installer artifact.Installer) int {
	parsed, err := versionpkg.ParseVersion(
		installer.Minecraft,
		versionpkg.DialectMinecraft,
	)
	if err != nil {
		return nativeInstallerJava
	}
	minecraft, ok := parsed.(*versionpkg.MinecraftVersion)
	if !ok || minecraft.Post26 {
		// A snapshot or a year-scheme release names no line this table
		// covers, so only the installer's own minimum is known.
		return nativeInstallerJava
	}
	switch {
	case minecraft.Year == 1 &&
		(minecraft.Update > 20 || minecraft.Update == 20 && minecraft.Hotfix >= 5):
		return 21
	case minecraft.Year == 1 && minecraft.Update >= 18:
		return 17
	case minecraft.Year == 1 && minecraft.Update >= 17:
		return 16
	default:
		return 8
	}
}
