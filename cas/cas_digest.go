// Package cas stores immutable contents independently of consumer metadata.
package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Digest is a SHA-256 address. Its serialized form includes the algorithm name.
type Digest [sha256.Size]byte

// ParseDigest accepts only the canonical sha256:<lowercase-hex> representation.
func ParseDigest(value string) (Digest, error) {
	var digest Digest
	if len(value) != 7+sha256.Size*2 || value[:7] != "sha256:" {
		return digest, fmt.Errorf("expected sha256:<64 lowercase hexadecimal characters>")
	}
	for i := range digest {
		high, highOK := hexDigit(value[7+i*2])
		low, lowOK := hexDigit(value[8+i*2])
		if !highOK || !lowOK {
			return Digest{}, fmt.Errorf("invalid sha256 digest")
		}
		digest[i] = high<<4 | low
	}
	return digest, nil
}

func hexDigit(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	default:
		return 0, false
	}
}

func (d Digest) String() string {
	var text [7 + sha256.Size*2]byte
	copy(text[:], "sha256:")
	hex.Encode(text[7:], d[:])
	return string(text[:])
}

// Hex is the filename-safe digest without its algorithm prefix.
func (d Digest) Hex() string {
	return hex.EncodeToString(d[:])
}

func (d Digest) MarshalText() ([]byte, error) {
	text := make([]byte, 7+sha256.Size*2)
	copy(text, "sha256:")
	hex.Encode(text[7:], d[:])
	return text, nil
}

func (d *Digest) UnmarshalText(text []byte) error {
	parsed, err := ParseDigest(string(text))
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Object records storage facts, not a consumer's filename or retention policy.
type Object struct {
	Digest Digest `json:"digest"`
	Size   int64  `json:"size"`
}
