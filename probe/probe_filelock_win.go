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

package probe

import (
	"os"
	"path"

	"github.com/mclucy/lucy/logger"

	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"golang.org/x/sys/windows"
)

// This is AI generated code, please check it before use. I have no knowledge to
// Windows syscall.
var checkServerFileLock = tools.Memoize(
	func() *types.ServerActivity {
		lockPath := path.Join(
			savePath(),
			"session.lock",
		)
		file, err := os.OpenFile(lockPath, os.O_RDWR, 0o666)
		defer tools.CloseReader(file, logger.Warn)

		if err != nil {
			return nil
		}

		err = windows.LockFileEx(
			windows.Handle(file.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0,
			1,
			0,
			&windows.Overlapped{},
		)
		if err != nil {
			var info windows.ByHandleFileInformation
			err = windows.GetFileInformationByHandle(
				windows.Handle(file.Fd()),
				&info,
			)
			if err == nil {
				return &types.ServerActivity{
					Active: true,
					Pid:    int(info.VolumeSerialNumber),
				}
			}
		}
		err = windows.UnlockFileEx(
			windows.Handle(file.Fd()),
			0,
			1,
			0,
			&windows.Overlapped{},
		)
		if err != nil {
			return nil
		}

		return &types.ServerActivity{
			Active: false,
			Pid:    0,
		}
	},
)
