//go:build unix || darwin || linux

/*
Copyright 2024 4rcadia

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package workspace

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
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
	defer fn.CloseReader(file, log.Warn)
	if err != nil {
		return false
	}

	log.Debug("checking lock on: " + file.Name())
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil && errors.Is(err, syscall.EWOULDBLOCK) {
		log.Debug("found a lock on the file: " + err.Error())
		return true
	} else if err != nil {
		return false
	}

	log.Debug("no lock found on the file: " + file.Name())
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if err != nil {
		log.Warn(err)
	}

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
	log.Debug("got output from lsof:\n" + out.String())

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
