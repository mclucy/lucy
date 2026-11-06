package bootstrap

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

// verifyArtifactHashes verifies every digest the lock pins for an artifact and
// returns the computed SHA-256. cachedSHA256, when supplied by a caller that
// already hashed the file, skips re-reading it.
func verifyArtifactHashes(path string, expectedHashes map[string]string, cachedSHA256 string) (string, error) {
	for algorithm := range expectedHashes {
		switch algorithm {
		case "md5", "sha1", "sha256", "sha512":
		default:
			return "", fmt.Errorf("unsupported artifact digest %s", algorithm)
		}
	}
	checks := [...]struct {
		name     string
		expected string
		digest   hash.Hash
	}{
		{name: "md5", expected: expectedHashes["md5"]},
		{name: "sha1", expected: expectedHashes["sha1"]},
		{name: "sha256", expected: expectedHashes["sha256"]},
		{name: "sha512", expected: expectedHashes["sha512"]},
	}
	if expectedHashes["md5"] == "" && expectedHashes["sha1"] == "" &&
		expectedHashes["sha256"] == "" && expectedHashes["sha512"] == "" {
		return "", errors.New("no expected hashes declared in lock")
	}
	var slots [4]io.Writer
	writers := slots[:0]
	for i := range checks {
		check := &checks[i]
		if check.name == "sha256" && cachedSHA256 != "" {
			if check.expected != "" && !strings.EqualFold(cachedSHA256, check.expected) {
				return "", fmt.Errorf("sha256 mismatch: got %s, want %s", cachedSHA256, check.expected)
			}
			continue
		}
		if check.expected == "" && check.name != "sha256" {
			continue
		}
		check.digest = newDigest(check.name)
		writers = append(writers, check.digest)
	}
	if len(writers) > 0 {
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		_, readErr := io.Copy(io.MultiWriter(writers...), file)
		if err := errors.Join(readErr, file.Close()); err != nil {
			return "", err
		}
	}
	sha256Hex := cachedSHA256
	for _, check := range checks {
		if check.digest == nil {
			continue
		}
		actual := hex.EncodeToString(check.digest.Sum(nil))
		if check.expected != "" && !strings.EqualFold(actual, check.expected) {
			return "", fmt.Errorf("%s mismatch: got %s, want %s", check.name, actual, check.expected)
		}
		if check.name == "sha256" {
			sha256Hex = actual
		}
	}
	return sha256Hex, nil
}

func newDigest(name string) hash.Hash {
	switch name {
	case "md5":
		return md5.New()
	case "sha1":
		return sha1.New()
	case "sha256":
		return sha256.New()
	case "sha512":
		return sha512.New()
	default:
		return nil
	}
}

// fileDigests hashes path with the requested algorithms. Asking for neither
// yields SHA-256, the digest callers always need.
func fileDigests(path string, needSHA256, needSHA1 bool) (sha256Hex string, sha1Hex string, err error) {
	if !needSHA256 && !needSHA1 {
		needSHA256 = true
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	var writers []io.Writer
	var h256, h1 hash.Hash
	if needSHA256 {
		h256 = sha256.New()
		writers = append(writers, h256)
	}
	if needSHA1 {
		h1 = sha1.New()
		writers = append(writers, h1)
	}
	if _, err := io.Copy(io.MultiWriter(writers...), f); err != nil {
		return "", "", err
	}
	if needSHA256 {
		sha256Hex = hex.EncodeToString(h256.Sum(nil))
	}
	if needSHA1 {
		sha1Hex = hex.EncodeToString(h1.Sum(nil))
	}
	return sha256Hex, sha1Hex, nil
}
