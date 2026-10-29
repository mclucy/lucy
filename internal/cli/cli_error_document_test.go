package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/resolve"
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
			name: "resolve no matching build",
			err: &resolve.Failure{
				Project:    "demo",
				Candidates: 3,
				Groups: []resolve.RejectionGroup{{
					Kind:         resolve.RejectionVersionMismatch,
					Detail:       "1.20",
					Count:        3,
					FirstVersion: "1.19",
					LastVersion:  "1.21",
				}},
			},
			want:     CodeResolveNoMatchingBuild,
			wantHint: "lucy add demo@1.19",
		},
		{
			name: "resolve no compatible build",
			err: &resolve.Failure{
				Project: "demo",
				Groups: []resolve.RejectionGroup{{
					Kind:   resolve.RejectionFilePattern,
					Detail: "*.jar",
					Count:  2,
				}},
			},
			want: CodeResolveNoCompatibleBuild,
		},
		{
			name: "resolve aborted",
			err: &resolve.Failure{
				Aborted: true,
				Cause:   errors.New("graph stopped"),
			},
			want: CodeCommandFailed,
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
			document := newErrorDocument(nil, test.err, "lucy.log")
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
