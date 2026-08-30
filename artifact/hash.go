package artifact

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
)

// File is an artifact file on disk. It implements upstream.Hashable.
type File struct {
	Path string
}

// Sha1 returns the SHA-1 digest of the file's content.
func (f File) Sha1() ([sha1.Size]byte, error) {
	file, err := os.Open(f.Path)
	if err != nil {
		return [sha1.Size]byte{}, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return [sha1.Size]byte{}, fmt.Errorf("hash artifact: %w", err)
	}
	return [sha1.Size]byte(hasher.Sum(nil)), nil
}

// MurmurHash computes CurseForge's custom MurmurHash2 fingerprint of the
// file's content. It strips whitespace bytes (0x09, 0x0A, 0x0D, 0x20) before
// mixing.
func (f File) MurmurHash() (uint32, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return 0, fmt.Errorf("read artifact: %w", err)
	}
	return MurmurHashBytes(data), nil
}

// MurmurHashBytes computes CurseForge's custom MurmurHash2 fingerprint.
// It strips whitespace bytes (0x09, 0x0A, 0x0D, 0x20) before mixing.
func MurmurHashBytes(data []byte) uint32 {
	const multiplex uint32 = 1540483477
	normalizedLen := uint32(0)
	for _, value := range data {
		if !isCurseForgeWhitespace(value) {
			normalizedLen++
		}
	}

	hash := uint32(1) ^ normalizedLen
	var pending uint32
	var pendingBits uint32
	for _, value := range data {
		if isCurseForgeWhitespace(value) {
			continue
		}
		pending |= uint32(value) << pendingBits
		pendingBits += 8
		if pendingBits == 32 {
			word := pending * multiplex
			word = (word ^ word>>24) * multiplex
			hash = hash*multiplex ^ word
			pending = 0
			pendingBits = 0
		}
	}
	if pendingBits > 0 {
		hash = (hash ^ pending) * multiplex
	}

	hash = (hash ^ hash>>13) * multiplex
	return hash ^ hash>>15
}

func isCurseForgeWhitespace(b byte) bool {
	return b == 0x09 || b == 0x0A || b == 0x0D || b == 0x20
}
