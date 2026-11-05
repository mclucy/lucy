package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/mclucy/lucy/workspace/identify"
	"github.com/spf13/cobra"
)

const flagStatusFromLockName = "from-lock"

var statusJSON JSONResult[statusDocument]

func NewStatusCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	statusCmd := &cobra.Command{
		Use:     "status",
		Short:   help.StatusShort,
		Args:    cobra.NoArgs,
		Aliases: []string{"s"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			logo, _ := cmd.Flags().GetString(FlagLogo)
			if _, ok := terminal.ParseStatusLogoMode(logo); !ok {
				return fmt.Errorf("--logo must be one of none, small, large")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionStatus(cmd, inv, deps, args)
		},
	}
	statusJSON.Bind(contracts, statusCmd)
	AddLongFlag(statusCmd)
	AddLogoFlag(statusCmd)
	statusCmd.Flags().Bool(
		flagStatusFromLockName,
		false,
		"Render the server identity and packages from lucy-lock.yaml instead of the live directory",
	)
	_ = statusCmd.RegisterFlagCompletionFunc(
		FlagLogo,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := FilterByPrefix(StaticStatusLogoCandidates(), toComplete)
			return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	return statusCmd
}

func actionStatus(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	long, _ := cmd.Flags().GetBool(FlagLong)
	logo, _ := cmd.Flags().GetString(FlagLogo)
	logoMode, _ := terminal.ParseStatusLogoMode(logo)
	output := NewQueryOutput(cmd, inv)

	fromLock, _ := cmd.Flags().GetBool(flagStatusFromLockName)
	if fromLock {
		return actionStatusFromLock(cmd.Context(), output, inv, long, logoMode)
	}
	return actionStatusLive(cmd, output, inv, deps, long, logoMode)
}

// actionStatusLive observes the working directory and reports the runtime and
// packages found there. Selection uncertainty is part of the document, not a
// command failure: an empty, ambiguous, or declared-missing workspace still
// prints a useful status.
func actionStatusLive(
	cmd *cobra.Command,
	output *Output,
	inv *Invocation,
	deps *Dependencies,
	long bool,
	logoMode terminal.StatusLogoMode,
) error {
	ctx, stop := CommandContext(cmd)
	defer stop()
	root := inv.Directory
	observed, identifyWarnings := identify.Packages(
		ctx,
		workspace.Observe(
			root,
			discovery.Request{Policy: discovery.PolicyPreferModified},
		),
		deps.Router.ResolveArtifactByHash,
	)
	if err := ctx.Err(); err != nil {
		return err
	}
	reportObservationFindings(observed, output)
	for _, warning := range identifyWarnings {
		output.Warn(warning)
	}
	if failure := statusSelectionFailure(observed); failure != nil {
		recordWorkspaceTelemetryFailure(inv)
		return failure
	}

	document := newLiveStatusDocument(observed, root)
	if document.Server != nil &&
		(document.Server.Product.Version == "" ||
			document.Server.Product.Version == versionpkg.Unknown.String()) {
		// Paperclip carries no build number in its metadata, and an installed
		// core may be renamed. The lock records the adopted build.
		if lock, err := documentpkg.LockFile(root).Read(); err == nil && lock.Server.CoreVersion != "" {
			document.Server.Product.Version = lock.Server.CoreVersion.String()
		}
	}
	if document.MCDR == nil ||
		document.MCDR.Version == "" ||
		document.MCDR.Version == versionpkg.Unknown.String() {
		// MCDReforged is provisioned locally, so a missing global executable
		// leaves the observation unknown. The lock carries the managed version.
		if lock, err := documentpkg.LockFile(root).Read(); err == nil && lock.MCDR != nil {
			document.MCDR = &statusMCDR{
				Directory: statusRelativePath(root, lock.MCDR.Directory),
				Version:   lock.MCDR.Version.String(),
			}
		}
	}
	recordWorkspaceTelemetry(inv, observed, document)
	document.Warnings = append(document.Warnings, output.Warnings()...)
	return writeStatusDocument(ctx, output, inv, document, long, logoMode)
}

// actionStatusFromLock reports only the facts persisted in lucy-lock.yaml.
// The lock cannot prove whether the server is running or which entry point an
// installation would use, so the document leaves both unobserved.
func actionStatusFromLock(
	ctx context.Context,
	output *Output,
	inv *Invocation,
	long bool,
	logoMode terminal.StatusLogoMode,
) error {
	root := inv.Directory
	lock, err := documentpkg.LockFile(root).Read()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &report.Detail{
				Title: fmt.Sprintf("no %s to render", documentpkg.LockFilename),
				Lines: []string{
					"The workspace has no lock file yet.",
				},
				Hint:  "Run lucy install first.",
				Code:  statusErrorLockMissing,
				Cause: err,
			}
		}
		return &report.Detail{
			Title: "cannot read " + documentpkg.LockFilename,
			Lines: []string{err.Error()},
			Code:  statusErrorReadFailed,
			Cause: err,
		}
	}
	return writeStatusDocument(
		ctx,
		output,
		inv,
		newLockStatusDocument(root, lock),
		long,
		logoMode,
	)
}

// statusSelectionFailure reports only selection outcomes that make the
// observation unusable. An empty workspace, an unresolved declaration, and
// competing candidates remain successful reports; discovery already turns
// them into document warnings.
func statusSelectionFailure(observed *workspace.Workspace) error {
	if observed == nil {
		return nil
	}
	failure := observed.SelectionError()
	if failure == nil {
		return nil
	}
	var typed *discovery.SelectionError
	if errors.As(failure, &typed) {
		switch typed.State {
		case discovery.SelectionNone,
			discovery.SelectionAmbiguous,
			discovery.SelectionMissingDeclared:
			return nil
		}
	}
	return &report.Detail{
		Title: "cannot select a server runtime",
		Lines: []string{failure.Error()},
		Code:  statusErrorSelectionFailed,
		Cause: failure,
	}
}

func writeStatusDocument(
	ctx context.Context,
	output *Output,
	inv *Invocation,
	document statusDocument,
	long bool,
	logoMode terminal.StatusLogoMode,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if output.JSON() {
		return statusJSON.Write(output, document)
	}
	_, err := fmt.Fprintln(
		output.Human(),
		renderStatus(document, long, logoMode, inv.Stdout, inv.Theme),
	)
	if err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}
