package javainstall

import (
	"context"
	"io"
	"path/filepath"
	"runtime"

	"github.com/mclucy/lucy/log"
)

// systemDefaultJava returns the resolved target of /usr/bin/java, or an empty
// string when the host has no default Java.
func systemDefaultJava() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	target, err := filepath.EvalSymlinks("/usr/bin/java")
	if err != nil {
		return ""
	}
	return target
}

// restoreSystemDefault points the java alternative back at previous when a
// package moved it. It does nothing when the default did not change.
func restoreSystemDefault(
	ctx context.Context,
	previous string,
	host Host,
	out io.Writer,
) error {
	if runtime.GOOS != "linux" || previous == "" {
		return nil
	}
	current := systemDefaultJava()
	if filepath.Clean(current) == filepath.Clean(previous) {
		return nil
	}
	log.Info("restoring system default java", "path", previous)
	return runStep(
		ctx,
		Step{
			Args: []string{
				"update-alternatives",
				"--set",
				"java",
				previous,
			},
			Elevation: ElevationRoot,
		},
		host,
		out,
	)
}
