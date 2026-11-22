//go:build linux

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
		MtimeNS: stat.Mtim.Sec*1_000_000_000 + stat.Mtim.Nsec,
		CtimeNS: stat.Ctim.Sec*1_000_000_000 + stat.Ctim.Nsec,
	}, true
}
