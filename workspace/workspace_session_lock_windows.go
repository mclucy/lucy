//go:build windows

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
	"os"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
	"golang.org/x/sys/windows"
)

// checkSessionLock reports whether another process holds an exclusive lock
// on session.lock. A successful exclusive lock means nobody else holds the
// file. A failed lock attempt means a live server holds the file.
func checkSessionLock(lockPath string) bool {
	file, err := os.OpenFile(lockPath, os.O_RDWR, 0o666)
	defer fn.CloseReader(file, log.Warn)
	if err != nil {
		return false
	}

	err = windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1,
		0,
		&windows.Overlapped{},
	)
	if err == nil {
		if unlockErr := windows.UnlockFileEx(
			windows.Handle(file.Fd()),
			0,
			1,
			0,
			&windows.Overlapped{},
		); unlockErr != nil {
			log.Warn(unlockErr)
		}
		return false
	}
	return true
}
