package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mclucy/lucy/internal/cipher"
)

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
		ciphertext, err := cipher.Encrypt(*encrypt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("cipher_ciphertext=%s\n", ciphertext)
	default:
		flag.Usage()
	}
}
