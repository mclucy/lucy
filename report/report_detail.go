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
	// Code is a stable machine-readable failure code. Human rendering ignores
	// it; commands that emit JSON carry it into the error document.
	Code  string
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

// RenderDetail writes one detail to w with the supplied theme. Every surface
// that reports failures uses it, so a failure reads the same wherever it
// appears.
func RenderDetail(w io.Writer, detail *Detail, theme style.Theme) {
	if detail == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", theme.Failure("error:"), detail.Title)
	for _, line := range detail.Lines {
		_, _ = fmt.Fprintf(w, "  %s %s\n", theme.Muted("│"), line)
	}
	if detail.Hint != "" {
		_, _ = fmt.Fprintf(w, "  %s %s\n", theme.Note("hint:"), detail.Hint)
	}
}
