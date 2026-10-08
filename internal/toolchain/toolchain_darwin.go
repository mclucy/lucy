package toolchain

import (
	"context"
	"os/exec"
)

const (
	javaExecutableName  = "java"
	javacExecutableName = "javac"
)

func systemJavaRoots() []string {
	return []string{"/Library/Java/JavaVirtualMachines", "/opt/homebrew/opt", "/usr/local/opt"}
}

func probeCommand(ctx context.Context, executable string, arguments ...string) *exec.Cmd {
	return exec.CommandContext(ctx, executable, arguments...)
}
