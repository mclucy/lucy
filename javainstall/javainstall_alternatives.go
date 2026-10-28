package javainstall

import (
	"context"
	"io"
	"path/filepath"
)

// systemDefaultJava returns the resolved target of /usr/bin/java, or an empty
// string when the host has no default Java.
func systemDefaultJava(host *Host) string {
	if host.goos != "linux" {
		return ""
	}
	target, err := filepath.EvalSymlinks("/usr/bin/java")
	if err != nil {
		return ""
	}
	return target
}

// restoreSystemDefault points the java alternative back at previous when a
// package moved it. It does nothing when the default did not change, and it
// uses the invocation's elevation facts.
func restoreSystemDefault(
	ctx context.Context,
	previous string,
	policy Policy,
	host *Host,
	input io.Reader,
	out io.Writer,
) error {
	if host.goos != "linux" || previous == "" {
		return nil
	}
	current := systemDefaultJava(host)
	if filepath.Clean(current) == filepath.Clean(previous) {
		return nil
	}
	host.logger.Info("restoring system default java", "path", previous)
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
		policy,
		host,
		input,
		out,
	)
}
