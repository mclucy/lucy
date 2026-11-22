//go:build !darwin && !linux && !windows

package toolchain

import "os"

// identityOf reads the stat identity of one inspected file. A platform without
// device or inode numbers identifies a file by its size and change timestamp.
func identityOf(info os.FileInfo) fileIdentity {
	return fileIdentity{Size: info.Size(), Modified: info.ModTime().UnixNano()}
}
