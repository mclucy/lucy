package fsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Barrier persists the writes to file in order: every write issued before it
// reaches stable storage before any write issued after it. A power loss can
// revert writes after the last Flush, but only as a suffix, which recovery
// treats as an interrupted operation. A full cache flush per file, which
// os.File.Sync issues on darwin, costs milliseconds per call.
func Barrier(file *os.File) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var barrierErr error
	if err := conn.Control(func(fd uintptr) {
		for {
			_, barrierErr = unix.FcntlInt(fd, unix.F_BARRIERFSYNC, 0)
			if !errors.Is(barrierErr, unix.EINTR) {
				return
			}
		}
	}); err != nil {
		return err
	}
	if barrierErr == nil {
		return nil
	}
	// A file system without barrier support, such as an SMB mount, gets the
	// full flush.
	return file.Sync()
}

// Flush makes every write that reached the device of file before the call
// durable.
func Flush(file *os.File) error {
	return file.Sync()
}
