//go:build e2e && linux

package mutation

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Unreaped zombies retain their process group but cannot modify the workspace.
func liveProcessGroup(group int) (bool, error) {
	if err := syscall.Kill(-group, 0); errors.Is(err, syscall.ESRCH) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, fmt.Errorf("read process list: %w", err)
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		state, processGroup, err := linuxProcessState(pid)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return false, err
		}
		if processGroup == group && state != "Z" && state != "X" {
			return true, nil
		}
	}
	return false, nil
}

func liveProcess(pid int) (bool, error) {
	state, _, err := linuxProcessState(pid)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return state != "Z" && state != "X", nil
}

func linuxProcessState(pid int) (string, int, error) {
	path := filepath.Join("/proc", strconv.Itoa(pid), "stat")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read process %d state: %w", pid, err)
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return "", 0, fmt.Errorf("invalid process %d state", pid)
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 3 {
		return "", 0, fmt.Errorf("invalid process %d state", pid)
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return "", 0, fmt.Errorf("parse process %d group: %w", pid, err)
	}
	return fields[0], group, nil
}

func TestZombieProcessGroupIsNotLive(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "exit 0")
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Wait() })
	pid := command.Process.Pid
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, _, err := linuxProcessState(pid)
		if err != nil {
			t.Fatal(err)
		}
		if state == "Z" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d did not become a zombie", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if live, err := liveProcess(pid); err != nil || live {
		t.Fatalf("zombie process %d: live=%t, err=%v", pid, live, err)
	}
	if live, err := liveProcessGroup(pid); err != nil || live {
		t.Fatalf("zombie process group %d: live=%t, err=%v", pid, live, err)
	}
}
