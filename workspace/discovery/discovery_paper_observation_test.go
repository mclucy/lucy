package discovery

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func paperFamilyFixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("cannot locate test file path")
	}
	return filepath.Clean(
		filepath.Join(
			filepath.Dir(file),
			"testdata",
			"paper_family",
		),
	)
}

func paperFamilyFixturePath(
	t *testing.T,
	lineage string,
	parts ...string,
) string {
	t.Helper()
	segments := append([]string{paperFamilyFixtureRoot(t), lineage}, parts...)
	return filepath.Join(segments...)
}

func writeFixtureJar(t *testing.T, sourceDir string, name string) string {
	t.Helper()

	jarPath := filepath.Join(t.TempDir(), name)
	file, err := os.Create(jarPath)
	if err != nil {
		t.Fatalf("create fixture jar: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	err = filepath.WalkDir(
		sourceDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(sourceDir, path)
			if err != nil {
				return err
			}

			entry, err := writer.Create(filepath.ToSlash(rel))
			if err != nil {
				return err
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, err = entry.Write(data)
			return err
		},
	)
	if err != nil {
		t.Fatalf("walk fixture tree: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close fixture jar writer: %v", err)
	}

	return jarPath
}
