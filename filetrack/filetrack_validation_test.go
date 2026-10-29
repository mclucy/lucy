package filetrack

import (
	"testing"

	documentpkg "github.com/mclucy/lucy/document"
)

func TestValidateRequest(t *testing.T) {
	t.Parallel()
	digest := behaviorDigest([]byte("request validation"))
	tests := []struct {
		name    string
		request Request
		wantErr bool
	}{
		{
			name:    "track one path",
			request: Request{Operation: Track, Paths: []string{"a.txt"}},
		},
		{
			name:    "unknown operation",
			request: Request{Operation: "unknown", Paths: []string{"a.txt"}},
			wantErr: true,
		},
		{
			name:    "all with path",
			request: Request{Operation: Update, All: true, Paths: []string{"a.txt"}},
			wantErr: true,
		},
		{
			name:    "all track",
			request: Request{Operation: Track, All: true},
			wantErr: true,
		},
		{
			name:    "all update",
			request: Request{Operation: Update, All: true},
		},
		{
			name:    "all recover",
			request: Request{Operation: Recover, All: true},
		},
		{
			name:    "missing path",
			request: Request{Operation: Update},
			wantErr: true,
		},
		{
			name:    "from source update",
			request: Request{Operation: Update, Paths: []string{"a.txt"}, FromSource: true},
		},
		{
			name:    "from source track",
			request: Request{Operation: Track, Paths: []string{"a.txt"}, FromSource: true},
			wantErr: true,
		},
		{
			name:    "immutable track",
			request: Request{Operation: Track, Paths: []string{"a.txt"}, Immutable: true},
		},
		{
			name:    "immutable update",
			request: Request{Operation: Update, Paths: []string{"a.txt"}, Immutable: true},
			wantErr: true,
		},
		{
			name:    "no hash track",
			request: Request{Operation: Track, Paths: []string{"a.txt"}, NoHash: true},
		},
		{
			name:    "no hash with immutable",
			request: Request{Operation: Track, Paths: []string{"a.txt"}, NoHash: true, Immutable: true},
			wantErr: true,
		},
		{
			name:    "no hash with recover url",
			request: Request{Operation: Track, Paths: []string{"a.txt"}, NoHash: true, RecoverURL: "https://example.com/a.txt"},
			wantErr: true,
		},
		{
			name:    "recover url without track",
			request: Request{Operation: Update, Paths: []string{"a.txt"}, RecoverURL: "https://example.com/a.txt"},
			wantErr: true,
		},
		{
			name:    "hash revert",
			request: Request{Operation: Revert, Paths: []string{"a.txt"}, Hash: digest},
		},
		{
			name:    "revision revert",
			request: Request{Operation: Revert, Paths: []string{"a.txt"}, Revision: 2},
		},
		{
			name:    "hash and revision",
			request: Request{Operation: Revert, Paths: []string{"a.txt"}, Hash: digest, Revision: 2},
			wantErr: true,
		},
		{
			name:    "hash without revert",
			request: Request{Operation: Update, Paths: []string{"a.txt"}, Hash: digest},
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateRequest(test.request)
			if test.wantErr && err == nil {
				t.Fatalf("validateRequest(%+v) accepted an invalid request", test.request)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateRequest(%+v) = %v", test.request, err)
			}
		})
	}
}

func TestSelectFiles(t *testing.T) {
	t.Parallel()
	declared := []documentpkg.TrackedFile{
		{Path: "a.txt", Hash: behaviorDigest([]byte("a"))},
		{Path: "b.txt", Hash: behaviorDigest([]byte("b")), RecoverURL: "https://example.com/b.txt"},
		{Path: "c.txt"},
	}
	tests := []struct {
		name     string
		request  Request
		selected []string
		skipped  []string
		wantErr  bool
	}{
		{
			name:     "deduplicates explicit paths",
			request:  Request{Operation: Update, Paths: []string{"a.txt", "a.txt"}},
			selected: []string{"a.txt"},
		},
		{
			name:    "rejects tracked path",
			request: Request{Operation: Track, Paths: []string{"a.txt"}},
			wantErr: true,
		},
		{
			name:    "rejects untracked path",
			request: Request{Operation: Update, Paths: []string{"missing.txt"}},
			wantErr: true,
		},
		{
			name:     "all update selects everything",
			request:  Request{Operation: Update, All: true},
			selected: []string{"a.txt", "b.txt", "c.txt"},
		},
		{
			name:     "all update from source skips files without a URL",
			request:  Request{Operation: Update, All: true, FromSource: true},
			selected: []string{"b.txt"},
			skipped:  []string{"a.txt", "c.txt"},
		},
		{
			name:     "all recover skips presence-only files",
			request:  Request{Operation: Recover, All: true},
			selected: []string{"a.txt", "b.txt"},
			skipped:  []string{"c.txt"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			selected, skipped, err := selectFiles(declared, test.request)
			if test.wantErr {
				if err == nil {
					t.Fatalf("selectFiles(%+v) accepted an invalid request", test.request)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectFiles(%+v) = %v", test.request, err)
			}
			if len(selected) != len(test.selected) {
				t.Fatalf("selected = %v, want %v", selected, test.selected)
			}
			for i := range test.selected {
				if selected[i] != test.selected[i] {
					t.Fatalf("selected = %v, want %v", selected, test.selected)
				}
			}
			if len(skipped) != len(test.skipped) {
				t.Fatalf("skipped = %v, want %v", skipped, test.skipped)
			}
			for i := range test.skipped {
				if skipped[i] != test.skipped[i] {
					t.Fatalf("skipped = %v, want %v", skipped, test.skipped)
				}
			}
		})
	}
}
