package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mclucy/lucy/report"
)

func TestErrorDocumentCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		want     string
		wantHint string
	}{
		{
			name: "detail code",
			err: &report.Detail{
				Title: "no compatible build",
				Lines: []string{"paper: 1.21.4"},
				Hint:  "choose another version",
				Code:  CodeResolveNoCompatibleBuild,
			},
			want:     CodeResolveNoCompatibleBuild,
			wantHint: "choose another version",
		},
		{
			name:     "usage",
			err:      errors.New("unknown flag: --bogus"),
			want:     CodeInvalidInvocation,
			wantHint: "Run --help for usage.",
		},
		{
			name: "cancelled",
			err:  fmt.Errorf("resolve: %w", context.Canceled),
			want: CodeOperationCancelled,
		},
		{
			name: "generic",
			err:  errors.New("disk on fire"),
			want: CodeCommandFailed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := newErrorDocument(test.err, "lucy.log")
			if document.Code != test.want {
				t.Fatalf("code = %q, want %q", document.Code, test.want)
			}
			if document.Details == nil {
				t.Fatal("details is nil, want an encoded empty list")
			}
			if document.Title == "" {
				t.Fatal("title is empty, want the failure summary")
			}
			if document.Schema != ErrorSchema || document.Kind != ErrorKind {
				t.Fatalf(
					"document = {schema %d, kind %q}, want {schema %d, kind %q}",
					document.Schema,
					document.Kind,
					ErrorSchema,
					ErrorKind,
				)
			}
			if document.Hint != test.wantHint {
				t.Fatalf("hint = %q, want %q", document.Hint, test.wantHint)
			}
			if document.Log != "lucy.log" {
				t.Fatalf("log = %q, want %q", document.Log, "lucy.log")
			}
		})
	}
}
