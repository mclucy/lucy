//go:build windows

package toolchain

import (
	"os"
	"syscall"
)

// identityOf reads the stat identity of one inspected file. Windows exposes no
// device or inode number here, so the size and both timestamps identify it.
func identityOf(info os.FileInfo) fileIdentity {
	identity := fileIdentity{Size: info.Size(), Modified: info.ModTime().UnixNano()}
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		identity.Modified = data.LastWriteTime.Nanoseconds()
		identity.Changed = data.CreationTime.Nanoseconds()
	}
	return identity
}
