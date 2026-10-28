package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/shell"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/mclucy/lucy/version"
	"github.com/spf13/cobra"
)

// createOutput reuses install progress and warning plumbing, but owns the
// create summary and failure wording.
type createOutput struct {
	*installOutput
}

// createJSON is the machine-readable result of one creation.
var createJSON JSONResult[*operation.CreateResult]

func newCreateOutput(cmd *cobra.Command, inv *Invocation) *createOutput {
	return &createOutput{installOutput: newInstallOutput(cmd, inv)}
}

// Render prints one creation outcome. JSON goes to stdout; human output goes
// to stderr, like every other mutating command.
func (o *createOutput) Render(result *operation.CreateResult) error {
	if result == nil {
		return nil
	}
	result.WarningMessages = append(result.WarningMessages, o.Warnings()...)
	if o.JSON() {
		return createJSON.Write(o.Output, result)
	}
	lines := createSummaryLines(result, o.inv.Theme, o.inv.Directory)
	o.renderer.Println(append([]string{""}, lines...)...)
	o.Close()
	for _, warning := range result.Warnings {
		o.Warn(warning)
	}
	for _, diagnostic := range result.Diagnostics {
		o.Warn(fmt.Errorf("%s: %s", diagnostic.Path, diagnostic.Message))
	}
	return nil
}

// Failure adds the state that a create failure leaves behind. The shared
// install presenter explains the operation error itself.
func (o *createOutput) Failure(err error) error {
	if err == nil {
		return nil
	}
	if resolveErr, ok := errors.AsType[*operation.CreateResolveError](err); ok {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return &report.Detail{
				Title: "creation cancelled",
				Lines: []string{"No manifest was written."},
				Cause: err,
			}
		}
		detail := &report.Detail{
			Title: "could not create the " + createProductTitle(string(resolveErr.Product)) + " server",
			Lines: []string{
				"Directory: " + displayCreateLocation(resolveErr.Workspace, o.inv.Directory),
				"Minecraft: " + versionLabel(resolveErr.Minecraft),
				"Server version: " + versionLabel(resolveErr.Selector),
			},
			Cause: err,
		}
		if inner := createErrorDetail(o, resolveErr.Err); inner != nil {
			detail.Lines = append(detail.Lines, inner.Lines...)
			detail.Hint = inner.Hint
		} else {
			detail.Lines = append(detail.Lines, resolveErr.Err.Error())
		}
		if detail.Hint == "" {
			switch {
			case resolveErr.Product == serverpkg.ProductPaper:
				detail.Hint = "Paper versions are build numbers; try 232, stable, or any."
			case resolveErr.Product == serverpkg.ProductPurpur:
				detail.Hint = "Purpur versions are build numbers or latest."
			default:
				detail.Hint = "Check the server software version and the Minecraft version, then retry."
			}
		}
		return detail
	}
	if publishErr, ok := errors.AsType[*operation.CreatePublishError](err); ok {
		detail := &report.Detail{
			Title: "could not write the server directory",
			Lines: []string{"Directory: " + displayCreatePath(publishErr.Workspace, o.inv.Directory)},
			Cause: err,
		}
		if publishErr.ManifestWritten {
			detail.Lines = append(detail.Lines, "lucy.yaml was written, but lucy-lock.yaml was not.")
			detail.Hint = "Run " + createInstallCommand(publishErr.Workspace, o.inv.Directory) + " to finish resolving the lock."
		} else {
			detail.Lines = append(detail.Lines, "No manifest or lock was written.")
			detail.Hint = "Check that the directory is writable and try again."
		}
		detail.Lines = append(detail.Lines, publishErr.Err.Error())
		return detail
	}
	if installErr, ok := errors.AsType[*operation.CreateInstallError](err); ok {
		detail := &report.Detail{
			Title: "the server directory was created but installation failed",
			Lines: []string{
				"Directory: " + displayCreateLocation(installErr.Workspace, o.inv.Directory),
				"lucy.yaml and lucy-lock.yaml were written.",
			},
			Cause: err,
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			detail.Title = "installation interrupted"
			detail.Lines = append(detail.Lines, "The server directory was created; installation did not finish.")
		}
		if _, committed := errors.AsType[*install.CommittedError](installErr.Err); committed {
			detail.Title = "the server directory was created but provisioning was interrupted"
			detail.Lines = append(detail.Lines, "The installation was committed; file provisioning did not finish.")
		}
		if inner := createErrorDetail(o, installErr.Err); inner != nil {
			detail.Lines = append(detail.Lines, inner.Lines...)
			detail.Hint = inner.Hint
		} else {
			detail.Lines = append(detail.Lines, installErr.Err.Error())
		}
		if detail.Hint == "" {
			detail.Hint = "Run " + createInstallCommand(installErr.Workspace, o.inv.Directory) + " to finish."
		}
		message := installErr.Err.Error()
		switch {
		case errors.Is(installErr.Err, bootstrap.ErrNoJava):
			detail.Hint = "Run " + createInstallCommand(installErr.Workspace, o.inv.Directory) +
				" --java to install the Java runtime the server asks for."
		case strings.Contains(message, "unowned or modified output conflict"):
			detail.Hint = "Move the existing file out of the way, or run lucy init to adopt the existing server."
		case strings.Contains(message, "stop the running Minecraft server"):
			detail.Hint = "Stop the server, then run " + createInstallCommand(installErr.Workspace, o.inv.Directory) + "."
		}
		return detail
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &report.Detail{
			Title: "creation cancelled",
			Lines: []string{"No manifest was written."},
			Cause: err,
		}
	}
	return o.installOutput.Failure(err)
}

func createSummaryLines(
	result *operation.CreateResult,
	theme style.Theme,
	base string,
) []string {
	path := displayCreateLocation(result.Workspace, base)
	title := createProductTitle(string(result.Server.Product))
	minecraft := string(result.Server.Minecraft)
	coreVersion := string(result.Server.Version)
	if coreVersion == "" {
		coreVersion = minecraft
	}
	var headline string
	custom := result.Manifest != nil && result.Manifest.Server.Core.Repository != ""
	switch {
	case result.Server.Product == serverpkg.ProductMinecraft:
		headline = theme.Success("✓") + " Created a Minecraft " + minecraft + " server"
	case custom:
		headline = theme.Success("✓") + " Created " + title + " " +
			coreVersion + " for Minecraft " + minecraft
	default:
		headline = theme.Success("✓") + " Created " + title + " " + coreVersion +
			" for Minecraft " + minecraft
	}
	if path != "" {
		headline += " in " + path
	}
	written := "Wrote lucy.yaml"
	if result.ReplacedManifest {
		written = "Replaced lucy.yaml"
	}
	if result.PublishedLock {
		written += " and lucy-lock.yaml"
	} else {
		written += " (lucy-lock.yaml unchanged)"
	}
	lines := []string{
		headline,
		theme.Muted("  " + written),
	}
	if result.Installed {
		files := result.Files.Created + result.Files.Replaced
		switch {
		case files > 0:
			lines = append(lines, theme.Muted("  Installed "+fileCountLabel(files)))
		default:
			lines = append(lines, theme.Muted("  Server files are current"))
		}
		if result.Files.Removed > 0 {
			lines = append(lines, theme.Muted("  Removed "+fileCountLabel(result.Files.Removed)))
		}
	} else {
		lines = append(
			lines,
			"  "+theme.Note("Next:")+" "+createInstallCommand(result.Workspace, base),
		)
	}
	return lines
}

func createInstallCommand(workspace, base string) string {
	path := displayCreatePath(workspace, base)
	if path == "." || path == "" {
		return "lucy install"
	}
	if !filepath.IsAbs(path) {
		path = "./" + path
	}
	return "cd " + shell.Quote(path) + " && lucy install"
}

func fileCountLabel(count int) string {
	if count == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", count)
}

func versionLabel(value version.Version) string {
	text := strings.TrimSpace(value.String())
	if text == "" {
		return "any"
	}
	return text
}

// createErrorDetail uses the install presenter's richer failure mapping, then
// falls back to the generic resolution detail.
func createErrorDetail(o *createOutput, err error) *report.Detail {
	if inner := o.installOutput.Failure(err); inner != nil {
		if detail, ok := errors.AsType[*report.Detail](inner); ok {
			return detail
		}
	}
	return errorDetail(err)
}
