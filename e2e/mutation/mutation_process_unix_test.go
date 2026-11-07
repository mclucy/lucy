//go:build e2e && !windows

package mutation

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// osMissingFileText is the operating system's message for a missing file, as
// Lucy's error output carries it.
const osMissingFileText = "no such file or directory"

// processTree is the process group led by a started command.
type processTree struct {
	command *exec.Cmd
}

// startProcessTree starts command as the leader of a new process group. Context
// cancellation and stop SIGKILL that group. A descendant that calls setsid or
// setpgid leaves the group and is not reached.
func startProcessTree(command *exec.Cmd, interrupted *atomic.Bool) (*processTree, error) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	tree := &processTree{command: command}
	command.Cancel = func() error {
		err := tree.signal()
		if err == nil {
			interrupted.Store(true)
		}
		return err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	return tree, nil
}

// signal SIGKILLs the process group. It returns os.ErrProcessDone when the group
// has no members.
func (t *processTree) signal() error {
	err := syscall.Kill(-t.command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// stop kills live group members before the workspace is observed.
func (t *processTree) stop() (killed bool, err error) {
	live, err := liveProcessGroup(t.command.Process.Pid)
	if err != nil {
		return false, fmt.Errorf("inspect process group %d: %w", t.command.Process.Pid, err)
	}
	if !live {
		return false, nil
	}
	switch signalErr := t.signal(); {
	case signalErr == nil:
		killed = true
	case !errors.Is(signalErr, os.ErrProcessDone):
		return killed, signalErr
	}
	return killed, t.awaitEmpty()
}

func (t *processTree) awaitEmpty() error {
	group := t.command.Process.Pid
	deadline := time.Now().Add(processTreeDrainTimeout)
	for {
		live, err := liveProcessGroup(group)
		if err != nil {
			return fmt.Errorf("inspect process group %d: %w", group, err)
		}
		if !live {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("process group %d still has live members %s after SIGKILL",
				group, processTreeDrainTimeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// descendantPID reads the pid a shell script wrote to pidFile.
func descendantPID(t *testing.T, pidFile string) int {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read descendant pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse descendant pid %q: %v", raw, err)
	}
	return pid
}

// TestProcessTreeKilledOnTimeout runs a shell that starts a background
// descendant in the same group, then times out. The descendant must be gone
// when runProcess returns, because a snapshot taken after a timeout would
// otherwise race its writes.
func TestProcessTreeKilledOnTimeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "descendant.pid")
	const script = `sleep 300 & echo $! > "$1"; wait`
	result := runProcess(t, 3*time.Second, "/bin/sh",
		[]string{"-c", script, "sh", pidFile}, dir, os.Environ())
	if result.outcome.Status != statusTimeout {
		t.Fatalf("want a timeout, got %+v\nstderr:\n%s", result.outcome, result.stderr)
	}
	pid := descendantPID(t, pidFile)
	if live, err := liveProcess(pid); err != nil || live {
		t.Errorf("descendant %d survived the timeout: live=%t, err=%v", pid, live, err)
	}
}

// The leader exits before the deadline while its descendant keeps a pipe open
// beyond it; cleanup must not reclassify the observed exit as a timeout.
func TestProcessTreeKilledAfterNonzeroExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "descendant.pid")
	const script = `sleep 60 & echo $! > "$1"; exit 3`
	result := runProcess(t, time.Second, "/bin/sh",
		[]string{"-c", script, "sh", pidFile}, dir, os.Environ())
	if result.outcome.Status != statusExited ||
		result.outcome.ExitCode == nil || *result.outcome.ExitCode != 3 {
		t.Fatalf("want exit status 3, got %+v\nstderr:\n%s", result.outcome, result.stderr)
	}
	pid := descendantPID(t, pidFile)
	if live, err := liveProcess(pid); err != nil || live {
		t.Errorf("descendant %d survived the nonzero exit: live=%t, err=%v", pid, live, err)
	}
}
