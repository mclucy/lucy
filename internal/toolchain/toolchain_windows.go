package toolchain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	javaExecutableName  = "java.exe"
	javacExecutableName = "javac.exe"
)

func systemJavaRoots() []string {
	roots := make([]string, 0)
	for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		base := os.Getenv(variable)
		if base == "" {
			continue
		}
		for _, vendor := range []string{"Java", "Eclipse Adoptium", "Microsoft", "Amazon Corretto", "Azul Systems", "BellSoft"} {
			roots = append(roots, filepath.Join(base, vendor))
		}
	}
	return roots
}

func probeCommand(ctx context.Context, executable string, arguments ...string) *exec.Cmd {
	extension := strings.ToLower(filepath.Ext(executable))
	if extension != ".bat" && extension != ".cmd" {
		return exec.CommandContext(ctx, executable, arguments...)
	}
	quoted := make([]string, 0, len(arguments)+1)
	quoted = append(quoted, `"`+executable+`"`)
	for _, argument := range arguments {
		quoted = append(quoted, `"`+strings.ReplaceAll(argument, `"`, `""`)+`"`)
	}
	cmd := exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", executable)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /d /s /c "` + strings.Join(quoted, " ") + `"`}
	return cmd
}
