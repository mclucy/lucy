// Package report presents warnings, errors, and multi-line failures on
// stderr independently of diagnostic logging.
package report

import (
	"fmt"
	"io"

	"github.com/mclucy/lucy/terminal/style"
)

// Detail is a multi-line failure that the command boundary chooses to render
// instead of the framework's single-line box. Cause preserves the chain.
type Detail struct {
	Title string
	Lines []string
	Hint  string
	Cause error
}

func (d *Detail) Error() string {
	if d == nil {
		return ""
	}
	return d.Title
}

func (d *Detail) Unwrap() error {
	if d == nil {
		return nil
	}
	return d.Cause
}

// RenderDetail writes one detail to w. Every surface that reports failures
// uses it, so a failure reads the same wherever it appears.
func RenderDetail(w io.Writer, detail *Detail) {
	if detail == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", style.Failure("error:"), detail.Title)
	for _, line := range detail.Lines {
		_, _ = fmt.Fprintf(w, "  %s %s\n", style.Muted("│"), line)
	}
	if detail.Hint != "" {
		_, _ = fmt.Fprintf(w, "  %s %s\n", style.Note("hint:"), detail.Hint)
	}
}
