// Command cipher generates build-time embedded secret material.
//
// It never prints the plaintext secret. Output is four KEY=value lines for
// shell capture into linker-injected fragments.
//
// The secret is read from stdin only (never from argv), so it does not appear
// in process argument lists.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mclucy/lucy/internal/cipher"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s -encrypt-stdin [options] < secret\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nReads the plaintext secret from stdin, generates a fresh key, encrypts,\n")
		fmt.Fprintf(os.Stderr, "and prints four fragment lines:\n")
		fmt.Fprintf(os.Stderr, "  cipher_key_a, cipher_key_b, cipher_ciphertext_a, cipher_ciphertext_b\n")
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
	}

	encryptStdin := flag.Bool("encrypt-stdin", false, "read plaintext secret from stdin (required)")
	version := flag.String("version", "", "release version for AD binding (empty = unbound local)")
	commit := flag.String("commit", "", "release commit for AD binding (empty = unbound local)")
	flag.Parse()

	if !*encryptStdin {
		flag.Usage()
		os.Exit(2)
	}

	secret, err := readSecret(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cipher: read secret: %v\n", err)
		os.Exit(1)
	}
	if secret == "" {
		fmt.Fprintf(os.Stderr, "cipher: empty secret on stdin\n")
		os.Exit(2)
	}

	key, err := cipher.GenerateKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cipher: generate key: %v\n", err)
		os.Exit(1)
	}

	ctHex, err := cipher.Encrypt(key, secret, *version, *commit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cipher: encrypt: %v\n", err)
		os.Exit(1)
	}

	keyHex := hex.EncodeToString(key)
	keyA, keyB, err := splitExactHalves(keyHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cipher: split key: %v\n", err)
		os.Exit(1)
	}
	ctA, ctB, err := splitExactHalves(ctHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cipher: split ciphertext: %v\n", err)
		os.Exit(1)
	}

	// Four fragment lines only — never print the plaintext secret.
	fmt.Printf("cipher_key_a=%s\n", keyA)
	fmt.Printf("cipher_key_b=%s\n", keyB)
	fmt.Printf("cipher_ciphertext_a=%s\n", ctA)
	fmt.Printf("cipher_ciphertext_b=%s\n", ctB)
}

// readSecret reads all of r, trims a single trailing newline if present, and
// rejects empty input after trimming surrounding whitespace.
func readSecret(r io.Reader) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	// Preserve internal content; only strip one trailing \n or \r\n so secrets
	// are not altered beyond ordinary line-oriented stdin delivery.
	s := string(raw)
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	if strings.TrimSpace(s) == "" {
		return "", nil
	}
	return s, nil
}

// splitExactHalves splits s into two equal-length halves. s must have even length.
func splitExactHalves(s string) (string, string, error) {
	if len(s) == 0 {
		return "", "", fmt.Errorf("empty string")
	}
	if len(s)%2 != 0 {
		return "", "", fmt.Errorf("odd length %d", len(s))
	}
	mid := len(s) / 2
	return s[:mid], s[mid:], nil
}
