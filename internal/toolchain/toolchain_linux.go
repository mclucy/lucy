package toolchain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	javaExecutableName  = "java"
	javacExecutableName = "javac"
)

func systemJavaRoots() []string {
	roots := []string{"/usr/lib/jvm", "/usr/java", "/opt/java", "/home/linuxbrew/.linuxbrew/opt"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".linuxbrew", "opt"))
	}
	return roots
}

func probeCommand(ctx context.Context, executable string, arguments ...string) *exec.Cmd {
	return exec.CommandContext(ctx, executable, arguments...)
}
