//go:build darwin

package toolchain

import (
	"os"
	"syscall"
)

// identityOf reads the stat identity of one inspected file.
func identityOf(info os.FileInfo) fileIdentity {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{Size: info.Size(), Modified: info.ModTime().UnixNano()}
	}
	return fileIdentity{
		Device:   uint64(stat.Dev),
		Inode:    stat.Ino,
		Size:     stat.Size,
		Modified: stat.Mtimespec.Nano(),
		Changed:  stat.Ctimespec.Nano(),
	}
}
