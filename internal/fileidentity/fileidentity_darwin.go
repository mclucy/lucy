//go:build darwin

package fileidentity

import (
	"io/fs"
	"syscall"
)

func identityFromInfo(info fs.FileInfo) (Identity, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Identity{}, false
	}
	return Identity{
		Device:  uint64(stat.Dev),
		Inode:   stat.Ino,
		Size:    stat.Size,
		MtimeNS: stat.Mtimespec.Sec*1_000_000_000 + stat.Mtimespec.Nsec,
		CtimeNS: stat.Ctimespec.Sec*1_000_000_000 + stat.Ctimespec.Nsec,
	}, true
}
