package info

import (
	"context"
	"errors"
	"fmt"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/report"
)

// infoOperationFailure converts an interruption into the shared cancelled
// failure, including the cause that main maps to exit code 130.
func infoOperationFailure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx != nil && ctx.Err() != nil {
		return infoCancellation(ctx.Err())
	}
	if errors.Is(err, context.Canceled) {
		return infoCancellation(err)
	}
	return nil
}

func infoCancellation(cause error) *report.Detail {
	if cause == nil {
		cause = context.Canceled
	}
	return &report.Detail{
		Title: "info interrupted",
		Lines: []string{"The operation was cancelled."},
		Code:  cli.CodeOperationCancelled,
		Cause: cause,
	}
}

// infoNormalizeWarnings keeps the warning stream one line per entry and
// removes exact duplicates while preserving order.
func infoNormalizeWarnings(warnings []string) []string {
	normalized := make([]string, 0, len(warnings))
	seen := make(map[string]struct{}, len(warnings))
	for _, warning := range warnings {
		warning = infoCleanLine(warning)
		if warning == "" {
			continue
		}
		if _, exists := seen[warning]; exists {
			continue
		}
		seen[warning] = struct{}{}
		normalized = append(normalized, warning)
	}
	return normalized
}

func infoWriteDocument(
	ctx context.Context,
	output *cli.Output,
	inv *cli.Invocation,
	document infoDocument,
	warnings []string,
	long bool,
) error {
	if err := ctx.Err(); err != nil {
		return infoCancellation(err)
	}
	for _, warning := range infoNormalizeWarnings(warnings) {
		output.Warn(errors.New(warning))
	}
	document.Warnings = output.Warnings()
	if document.Entries == nil {
		document.Entries = []infoEntry{}
	}
	if document.Warnings == nil {
		document.Warnings = []string{}
	}

	if output.JSON() {
		if err := infoJSON.Write(output, document); err != nil {
			return infoWriteFailure(err)
		}
		return nil
	}
	rendered := renderInfoDocument(
		document,
		long,
		inv.Directory,
		infoRenderContext{theme: inv.Theme, terminal: inv.Stdout},
	)
	if rendered == "" {
		return nil
	}
	if _, err := fmt.Fprintln(output.Human(), rendered); err != nil {
		return infoWriteFailure(err)
	}
	return nil
}

func infoWriteFailure(err error) *report.Detail {
	return &report.Detail{
		Title: "cannot write info output",
		Lines: []string{err.Error()},
		Code:  infoErrorWrite,
		Cause: err,
	}
}
