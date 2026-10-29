package cache

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"
)

func TestGetFilenameFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "path", raw: "https://example.com/a/b.jar", want: "b.jar"},
		{name: "query ignored", raw: "https://example.com/a/b.jar?download=1", want: "b.jar"},
		{name: "trailing slash", raw: "https://example.com/a/", want: ""},
		{name: "empty path", raw: "https://example.com", want: ""},
		{name: "single segment", raw: "https://example.com", want: ""},
		{name: "invalid", raw: "https://example.com/%zz", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := getFilenameFromURL(test.raw); got != test.want {
				t.Fatalf("getFilenameFromURL(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestSpeculateFilenamePrefersHeaderThenURL(t *testing.T) {
	t.Parallel()
	parsed, err := url.Parse("https://example.com/a/url-name.jar")
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{
		Header:  http.Header{},
		Request: &http.Request{URL: parsed},
	}
	if got := speculateFilename(response); got != "url-name.jar" {
		t.Fatalf("filename without a header = %q, want the URL name", got)
	}

	response.Header.Set("Content-Disposition", `attachment; filename="header-name.jar"`)
	if got := speculateFilename(response); got != "header-name.jar" {
		t.Fatalf("filename with a header = %q, want the header name", got)
	}

	response.Header.Set("Content-Disposition", "not a media type")
	if got := speculateFilename(response); got != "url-name.jar" {
		t.Fatalf("malformed header fell back to %q, want the URL name", got)
	}
}

func TestSanitizeFilenameStripsTraversal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    string
		fallback string
		want     string
	}{
		{name: "plain", value: "artifact.jar", fallback: "fallback", want: "artifact.jar"},
		{name: "parent traversal", value: "../artifact.jar", fallback: "fallback", want: "artifact.jar"},
		{name: "nested", value: "nested/artifact.jar", fallback: "fallback", want: "artifact.jar"},
		{name: "absolute", value: filepath.Join(string(filepath.Separator), "artifact.jar"), fallback: "fallback", want: "artifact.jar"},
		{name: "dot", value: ".", fallback: "fallback", want: "fallback"},
		{name: "dot dot", value: "..", fallback: "fallback", want: "fallback"},
		{name: "empty", value: "", fallback: "fallback", want: "fallback"},
		{name: "separator", value: string(filepath.Separator), fallback: "fallback", want: "fallback"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeFilename(test.value, test.fallback); got != test.want {
				t.Fatalf("sanitizeFilename(%q, %q) = %q, want %q", test.value, test.fallback, got, test.want)
			}
		})
	}
}

func TestContainedUnderRequiresStrictChild(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	if !containedUnder(parent, filepath.Join(parent, "child")) {
		t.Fatal("containedUnder rejected a direct child")
	}
	if !containedUnder(parent, filepath.Join(parent, "nested", "child")) {
		t.Fatal("containedUnder rejected a nested child")
	}
	if containedUnder(parent, parent) {
		t.Fatal("containedUnder accepted the parent itself")
	}
	if containedUnder(parent, parent+"-sibling") {
		t.Fatal("containedUnder accepted a sibling with the same prefix")
	}
	if containedUnder(parent, filepath.Join(parent, "..", "outside")) {
		t.Fatal("containedUnder accepted a path that escapes through the parent")
	}
}
