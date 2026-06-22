package artifacthash

import (
	"crypto/sha1"
	"io"
	"os"
)

type File struct {
	Path string
}

type Bytes []byte

func (f File) Sha1() [sha1.Size]byte {
	file, err := os.Open(f.Path)
	if err != nil {
		return [sha1.Size]byte{}
	}
	defer file.Close()

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return [sha1.Size]byte{}
	}

	return [sha1.Size]byte(hasher.Sum(nil))
}

// MurmurHash computes CurseForge's custom MurmurHash2 fingerprint.
// It strips whitespace bytes (0x09, 0x0A, 0x0D, 0x20) before mixing.
func (f File) MurmurHash() uint32 {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return 0
	}
	return Bytes(data).MurmurHash()
}

func (b Bytes) MurmurHash() uint32 {
	const multiplex uint32 = 1540483477
	normalizedLen := uint32(0)
	for _, value := range b {
		if !isCurseForgeWhitespace(value) {
			normalizedLen++
		}
	}

	hash := uint32(1) ^ normalizedLen
	var pending uint32
	var pendingBits uint32
	for _, value := range b {
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
