package cli

import (
	"os/exec"

	"golang.org/x/sys/unix"
)

// awaitManagedExit uses a process-exit event without reaping the child.
func awaitManagedExit(cmd *exec.Cmd) error {
	fd, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	event := unix.Kevent_t{
		Ident: uint64(cmd.Process.Pid), Filter: unix.EVFILT_PROC,
		Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT,
	}
	for {
		events := make([]unix.Kevent_t, 1)
		n, err := unix.Kevent(fd, []unix.Kevent_t{event}, events, nil)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n > 0 && events[0].Flags&unix.EV_ERROR != 0 {
			return unix.Errno(events[0].Data)
		}
		if n > 0 {
			return nil
		}
	}
}
