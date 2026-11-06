package javainstall

import (
	"context"
	"io"
	"path/filepath"
	"time"
)

// restoreTimeout bounds a best-effort restore. Package execution may have ended
// in failure or cancellation, so restoration detaches from that cancellation;
// the bound keeps a stuck package tool or credential prompt from blocking exit.
const restoreTimeout = 30 * time.Second

// systemDefaultJava returns the resolved target of /usr/bin/java, or an empty
// string when the host has no default Java.
func systemDefaultJava(host *Host) string {
	if host.goos != "linux" {
		return ""
	}
	target, err := host.system.EvalSymlinks("/usr/bin/java")
	if err != nil {
		return ""
	}
	return target
}

// restoreSystemDefault points the java alternative back at previous when a
// package moved it. It does nothing when the default did not change. A restore
// runs even when the package execution failed or was canceled, so it detaches
// from that cancellation and is bounded by restoreTimeout. It uses the
// invocation's elevation facts.
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
	restoreCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		restoreTimeout,
	)
	defer cancel()
	return runStep(
		restoreCtx,
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
