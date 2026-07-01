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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
)

func buildServerFileLockStatus() *ServerActivity {
	inactive := &ServerActivity{Active: false, Pid: 0}

	if savePath() == "" {
		return inactive
	}

	lockPath := filepath.Join(
		savePath(),
		"session.lock",
	)
	// Try lsof before using the file lock check. As the file lock check is
	// tested to be unstable on linux (Ubuntu 20.04, Linux 5.15.0-48-generic).
	pid, _ := lsof(lockPath)
	if pid != 0 {
		return &ServerActivity{Active: true, Pid: pid}
	}

	file, err := os.OpenFile(lockPath, os.O_RDWR|os.O_APPEND, 0o666)
	defer fn.CloseReader(file, log.Warn)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return inactive
	} else if err != nil {
		return nil
	}

	log.Debug("checking lock on: " + file.Name())
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil && errors.Is(err, syscall.EWOULDBLOCK) {
		log.Debug("found a lock on the file: " + err.Error())
		fl := syscall.Flock_t{
			Type: syscall.F_WRLCK,
		}
		err = syscall.FcntlFlock(file.Fd(), syscall.F_GETLK, &fl)
		log.Warn(
			fmt.Errorf("activity detected but cannot get pid: %w", err),
		)
		if err != nil {
			return &ServerActivity{Active: true, Pid: 0}
		}
		return &ServerActivity{Active: true, Pid: int(fl.Pid)}
	} else if err != nil {
		return nil
	}

	log.Debug("no lock found on the file: " + file.Name())
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if err != nil {
		log.Warn(err)
	}

	return inactive
}

var checkServerFileLock = fn.Memoize(buildServerFileLockStatus)

func init() {
	resetProbeFileLockCache = func() {
		checkServerFileLock = fn.Memoize(buildServerFileLockStatus)
	}
}

func lsof(filePath string) (pid int, err error) {
	cmd := exec.Command("lsof", filePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		return 0, err
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
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "java" {
			return strconv.Atoi(fields[1])
		}
	}

	return 0, nil
}
