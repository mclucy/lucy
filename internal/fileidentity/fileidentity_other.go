//go:build !darwin && !linux

package fileidentity

import "io/fs"

// identityFromInfo reports false on a platform whose stat identity is not
// exposed here, so every observation reads the content.
func identityFromInfo(fs.FileInfo) (Identity, bool) {
	return Identity{}, false
}
