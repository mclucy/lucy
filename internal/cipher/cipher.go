// Package cipher provides runtime decryption for embedded secrets.
// Designed to deter casual extraction, not provide complete security.
//
// Build-time material is split into four linker fragments and optionally bound
// to a release identity (version + commit) via AEAD associated data.
package cipher

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
)

// Linker-injected material. Set only via -X at build time; never set in source.
// Four encrypted-material fragments are concatenated inside Decode only.
var (
	keyA        string
	keyB        string
	ciphertextA string
	ciphertextB string

	// Public release identity used as AEAD associated data.
	// Both empty = local unbound material; both set = tagged release.
	releaseVersion string
	releaseCommit  string
)

// Fixed domain separator for associated data. Bump if the AD format changes.
const adDomain = "lucy-cipher-v1"

// Decode decrypts the embedded ciphertext and returns the plaintext API key.
//
// Empty embedding (all four fragments empty) returns ("", nil).
// Partial material, bad hex, wrong key length, incomplete identity, or
// ciphertext that is too short returns an error (never panics).
func Decode() (string, error) {
	if keyA == "" && keyB == "" && ciphertextA == "" && ciphertextB == "" {
		return "", nil
	}
	if keyA == "" || keyB == "" || ciphertextA == "" || ciphertextB == "" {
		return "", errors.New("cipher: partial embedded material")
	}

	keyBytes, err := hex.DecodeString(keyA + keyB)
	if err != nil {
		return "", fmt.Errorf("cipher: malformed key hex: %w", err)
	}
	if len(keyBytes) != chacha20poly1305.KeySize {
		return "", errors.New("cipher: incorrect key length")
	}

	data, err := hex.DecodeString(ciphertextA + ciphertextB)
	if err != nil {
		return "", fmt.Errorf("cipher: malformed ciphertext hex: %w", err)
	}

	ad, err := associatedData(releaseVersion, releaseCommit)
	if err != nil {
		return "", err
	}

	aead, err := chacha20poly1305.NewX(keyBytes)
	if err != nil {
		return "", err
	}
	if len(data) < aead.NonceSize() {
		return "", errors.New("cipher: ciphertext too short")
	}

	nonce := data[:aead.NonceSize()]
	ct := data[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ct, ad)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// GenerateKey returns a fresh 32-byte key from crypto/rand.
func GenerateKey() ([]byte, error) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("cipher: generate key: %w", err)
	}
	return key, nil
}

// Encrypt encrypts plaintext with key, binding associated data for version and
// commit. Both identity fields must be empty (local unbound) or both nonempty
// (tagged release). Returns hex(nonce || ciphertext).
func Encrypt(key []byte, plaintext, version, commit string) (string, error) {
	if len(key) != chacha20poly1305.KeySize {
		return "", errors.New("cipher: incorrect key length")
	}
	ad, err := associatedData(version, commit)
	if err != nil {
		return "", err
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("cipher: read nonce: %w", err)
	}
	ct := aead.Seal(nil, nonce, []byte(plaintext), ad)
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return hex.EncodeToString(out), nil
}

// associatedData builds the canonical AD sequence:
//
//	lucy-cipher-v1 \0 <version> \0 <commit>
//
// Identity must be both empty or both nonempty.
func associatedData(version, commit string) ([]byte, error) {
	vEmpty := version == ""
	cEmpty := commit == ""
	if vEmpty != cEmpty {
		return nil, errors.New("cipher: incomplete release identity")
	}
	// domain + NUL + version + NUL + commit
	n := len(adDomain) + 1 + len(version) + 1 + len(commit)
	ad := make([]byte, 0, n)
	ad = append(ad, adDomain...)
	ad = append(ad, 0)
	ad = append(ad, version...)
	ad = append(ad, 0)
	ad = append(ad, commit...)
	return ad, nil
}
