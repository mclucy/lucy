// Package cipher provides runtime decryption for embedded secrets.
// Designed to deter casual extraction, not provide complete security.
//
// Usage:
//   - Build time: go run ./internal/cipher -encrypt YOUR_API_KEY > .cipher_ciphertext
//   - Runtime: key := cipher.Decode()
package cipher

import (
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/chacha20poly1305"
)

var (
	// Key is the decryption key embedded in the binary.
	// Generated at build time via go run ./cmd/cipher -keygen
	Key      = ""
	keyBytes []byte
)

func init() {
	if Key == "" {
		return
	}
	var err error
	keyBytes, err = hex.DecodeString(Key)
	if err != nil {
		panic(err)
	}
}

// Ciphertext is the encrypted API key embedded in the binary.
// Generated at build time via go run ./internal/cipher -encrypt YOUR_KEY
var Ciphertext = ""

// Decode decrypts the embedded ciphertext and returns the plaintext.
func Decode() (string, error) {
	if Ciphertext == "" {
		return "", nil
	}

	data, err := hex.DecodeString(Ciphertext)
	if err != nil {
		return "", err
	}

	aead, err := chacha20poly1305.NewX(keyBytes)
	if err != nil {
		return "", err
	}

	if len(data) < aead.NonceSize() {
		return "", err
	}

	nonce := data[:aead.NonceSize()]
	ciphertext := data[aead.NonceSize():]

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// Encrypt encrypts plaintext using the embedded Key and returns hex ciphertext.
func Encrypt(plaintext string) (string, error) {
	aead, err := chacha20poly1305.NewX(keyBytes)
	if err != nil {
		return "", err
	}

	var nonce [24]byte
	f, err := os.Open("/dev/urandom")
	if err != nil {
		return "", err
	}
	io.ReadFull(f, nonce[:])
	f.Close()

	ciphertext := aead.Seal(nil, nonce[:], []byte(plaintext), nil)
	result := make([]byte, 0, len(nonce)+len(ciphertext))
	result = append(result, nonce[:]...)
	result = append(result, ciphertext...)
	return hex.EncodeToString(result), nil
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nOptions:\n")
		fmt.Fprintf(os.Stderr, "  -keygen         Generate new random key\n")
		fmt.Fprintf(os.Stderr, "  -encrypt KEY   Encrypt KEY with Key\n")
		flag.PrintDefaults()
	}

	keygen := flag.Bool("keygen", false, "generate new key")
	encrypt := flag.String("encrypt", "", "encrypt API key")
	flag.Parse()

	switch {
	case *keygen:
		var data [32]byte
		f, err := os.Open("/dev/urandom")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		io.ReadFull(f, data[:])
		f.Close()
		fmt.Printf("cipher_key=%x\n", data)
		os.Exit(0)
	case *encrypt != "":
		aead, err := chacha20poly1305.NewX(keyBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		var nonce [24]byte
		f, err := os.Open("/dev/urandom")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		io.ReadFull(f, nonce[:])
		f.Close()

		ciphertext := aead.Seal(nil, nonce[:], []byte(*encrypt), nil)
		fmt.Printf("cipher_ciphertext=%x\n", nonce)
		fmt.Printf("%x\n", ciphertext)

	default:
		flag.Usage()
	}
}
