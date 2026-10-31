//go:build unix || darwin || linux

package workspace

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// checkSessionLock reports whether another process holds session.lock. A
// result of false covers every failure mode. An unreadable or unlocked file
// says nothing about a running server.
//
// lsof runs first. Plain flock probes were unstable on some Linux kernels.
// lsof gives a definite answer when a java process holds the file open.
func checkSessionLock(lockPath string) bool {
	if sessionLockHeldByLsof(lockPath) {
		return true
	}

	file, err := os.OpenFile(lockPath, os.O_RDWR|os.O_APPEND, 0o666)
	if err != nil {
		return false
	}
	defer file.Close()

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil && errors.Is(err, syscall.EWOULDBLOCK) {
		return true
	} else if err != nil {
		return false
	}

	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}

// sessionLockHeldByLsof asks the lsof tool whether any process holds
// filePath open.
func sessionLockHeldByLsof(filePath string) bool {
	cmd := exec.Command("lsof", filePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false
	}
	lines := strings.Split(out.String(), "\n")
	outputBegin := 0
	for i, line := range lines {
		if strings.Contains(line, "COMMAND") {
			outputBegin = i + 1
			break
		}
	}
	for _, line := range lines[outputBegin:] {
		fields := strings.Fields(line)
		// Only the server JVM counts as server activity. Other holders, such
		// as editors and shells, do not count.
		if len(fields) >= 2 &&
			fields[0] == "java" &&
			pidLike(fields[1]) {
			return true
		}
	}

	return false
}

func pidLike(field string) bool {
	_, err := strconv.Atoi(field)
	return err == nil
}
