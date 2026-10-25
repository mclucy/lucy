package artifact_test

import (
	"archive/zip"
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type zipFixtureEntry struct {
	name string
	data []byte
}

func zipFixture(t testing.TB, entries ...zipFixtureEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", entry.name, err)
		}
		if _, err := file.Write(entry.data); err != nil {
			t.Fatalf("write zip entry %q: %v", entry.name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip fixture: %v", err)
	}
	return buffer.Bytes()
}

func writeZipFixture(t testing.TB, path string, entries ...zipFixtureEntry) {
	t.Helper()
	if err := os.WriteFile(path, zipFixture(t, entries...), 0o644); err != nil {
		t.Fatalf("write fixture archive: %v", err)
	}
}

// fuzzArchivePath returns one archive path per fuzz target process. Iterations
// overwrite the same file, which bounds disk use during long campaigns. Fuzz
// targets must not call t.Parallel while sharing this path.
func fuzzArchivePath(f *testing.F, filename string) string {
	f.Helper()
	return filepath.Join(f.TempDir(), filename)
}

func jsonFixture(t testing.TB, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON fixture: %v", err)
	}
	return data
}

func yamlFixture(t testing.TB, value any) []byte {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatalf("marshal YAML fixture: %v", err)
	}
	return data
}

func clippedText(value string, limit int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

// printableText removes line and control runes so a generated YAML scalar
// round-trips its source text exactly apart from surrounding whitespace.
func printableText(value string, limit int) string {
	value = clippedText(value, limit)
	var builder strings.Builder
	builder.Grow(len(value))
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			continue
		}
		builder.WriteRune(character)
	}
	return builder.String()
}

func safeID(value, fallback string) string {
	value = strings.ToLower(clippedText(value, 128))
	var builder strings.Builder
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z',
			character >= '0' && character <= '9',
			character == '-':
			builder.WriteRune(character)
		}
		if builder.Len() >= 48 {
			return builder.String()
		}
	}
	if builder.Len() == 0 {
		return fallback
	}
	return builder.String()
}

func modulo(value, divisor int) int {
	value %= divisor
	if value < 0 {
		value += divisor
	}
	return value
}
