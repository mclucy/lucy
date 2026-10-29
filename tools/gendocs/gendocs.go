package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func main() {
	check := flag.Bool("check", false, "fail if generated docs.tar.gz differs from docs/ content")
	outPath := flag.String("out", "docs/docs.tar.gz", "output path for compressed documentation archive")
	docsDir := flag.String("docs", "docs", "path to documentation source directory")
	flag.Parse()

	archive, err := buildArchive(*docsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] build docs archive: %v\n", err)
		os.Exit(1)
	}

	if *check {
		existing, err := os.ReadFile(*outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ERROR] read %s: %v\n", *outPath, err)
			os.Exit(1)
		}
		if !bytes.Equal(archive, existing) {
			fmt.Fprintf(os.Stderr, "[ERROR] %s is out of date; run task docs:pack\n", *outPath)
			os.Exit(1)
		}
		fmt.Printf("[OK] %s is up to date\n", *outPath)
		return
	}

	if err := os.WriteFile(*outPath, archive, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] write %s: %v\n", *outPath, err)
		os.Exit(1)
	}
	fmt.Printf("[OK] Wrote %s (%d bytes)\n", *outPath, len(archive))
}

func buildArchive(rootDir string) ([]byte, error) {
	var filePaths []string
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".mdx") || strings.HasSuffix(path, ".draft.mdx") {
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		filePaths = append(filePaths, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(filePaths)

	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	// Deterministic gzip header: clear modification time
	gw.Header.ModTime = time.Time{}
	gw.Header.OS = 255 // unknown OS for reproducibility

	tw := tar.NewWriter(gw)
	for _, rel := range filePaths {
		data, err := os.ReadFile(filepath.Join(rootDir, rel))
		if err != nil {
			return nil, err
		}
		header := &tar.Header{
			Name:     filepath.ToSlash(rel),
			Mode:     0o644,
			Size:     int64(len(data)),
			ModTime:  time.Unix(0, 0),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(header); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
