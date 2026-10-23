package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/mclucy/lucy/workspace/identify"
	"github.com/spf13/cobra"
)

const flagStatusFromLockName = "from-lock"

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: help.StatusShort,
	Args:  cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		logo, _ := cmd.Flags().GetString(FlagLogo)
		if _, ok := terminal.ParseStatusLogoMode(logo); !ok {
			return fmt.Errorf("--logo must be one of none, small, large")
		}
		return nil
	},
	RunE: actionStatus,
}

func NewStatusCommand() *cobra.Command {
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

func actionStatus(cmd *cobra.Command, args []string) error {
	long, _ := cmd.Flags().GetBool(FlagLong)
	logo, _ := cmd.Flags().GetString(FlagLogo)
	logoMode, _ := terminal.ParseStatusLogoMode(logo)
	output := NewQueryOutput(cmd)

	fromLock, _ := cmd.Flags().GetBool(flagStatusFromLockName)
	if fromLock {
		return actionStatusFromLock(output, long, logoMode)
	}
	return actionStatusLive(output, long, logoMode)
}

// actionStatusLive observes the working directory and reports the runtime and
// packages found there. Selection uncertainty is part of the document, not a
// command failure: an empty, ambiguous, or declared-missing workspace still
// prints a useful status.
func actionStatusLive(
	output *Output,
	long bool,
	logoMode terminal.StatusLogoMode,
) error {
	root, err := statusWorkingDirectory()
	if err != nil {
		return err
	}
	observed, identifyWarnings := identify.Packages(workspace.Observe(
		root,
		discovery.Request{Policy: discovery.PolicyPreferModified},
	))
	reportObservationFindings(observed, output)
	for _, warning := range identifyWarnings {
		output.Warn(warning)
	}
	if failure := statusSelectionFailure(observed); failure != nil {
		return failure
	}

	document := newLiveStatusDocument(observed, root)
	document.Warnings = append(document.Warnings, output.Warnings()...)
	return writeStatusDocument(output, document, long, logoMode)
}

// actionStatusFromLock reports only the facts persisted in lucy-lock.yaml.
// The lock cannot prove whether the server is running or which entry point an
// installation would use, so the document leaves both unobserved.
func actionStatusFromLock(
	output *Output,
	long bool,
	logoMode terminal.StatusLogoMode,
) error {
	root, err := statusWorkingDirectory()
	if err != nil {
		return err
	}
	lock, err := lockfile.Read(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &report.Detail{
				Title: fmt.Sprintf("no %s to render", lockfile.Filename),
				Lines: []string{
					"The workspace has no lock file yet.",
				},
				Hint:  "Run lucy install first.",
				Code:  statusErrorLockMissing,
				Cause: err,
			}
		}
		return &report.Detail{
			Title: "cannot read " + lockfile.Filename,
			Lines: []string{err.Error()},
			Code:  statusErrorReadFailed,
			Cause: err,
		}
	}
	return writeStatusDocument(
		output,
		newLockStatusDocument(root, lock),
		long,
		logoMode,
	)
}

// statusWorkingDirectory is the document root. Every reported path is
// relative to it, whether the command observed the directory or read a lock.
func statusWorkingDirectory() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", &report.Detail{
			Title: "cannot determine the working directory",
			Lines: []string{err.Error()},
			Code:  statusErrorReadFailed,
			Cause: err,
		}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", &report.Detail{
			Title: "cannot resolve the working directory",
			Lines: []string{err.Error()},
			Code:  statusErrorReadFailed,
			Cause: err,
		}
	}
	return filepath.Clean(absolute), nil
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
	output *Output,
	document statusDocument,
	long bool,
	logoMode terminal.StatusLogoMode,
) error {
	if output.JSON() {
		return output.WriteJSON(document)
	}
	_, err := fmt.Fprintln(
		output.Human(),
		renderStatus(document, long, logoMode, style.TermWidth()),
	)
	if err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}

// selectedRuntimeComponents presents the runtime components the status
// document reports. Discovery keeps the primary identity separate and stores
// only additional evidence, so a vanilla primary contributes its Minecraft
// component here, and a loader primary contributes its loader component.
func selectedRuntimeComponents(
	selection *discovery.Selection,
) []catalog.VersionedRef {
	if selection == nil || !selection.IsValid() {
		return []catalog.VersionedRef{}
	}
	identity := selection.Installation.Identity
	components := make([]catalog.VersionedRef, 0, len(identity.Components)+2)
	components = append(components, identity.Components...)

	primary := identity.Primary
	switch strings.ToLower(primary.Name.String()) {
	case "minecraft":
		components = ensureRuntimeComponent(components, catalog.VersionedRef{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      "minecraft",
			Version:   primary.Version,
		})
	case "fabric", "fabric-loader":
		components = ensureRuntimeComponent(components, catalog.VersionedRef{
			Ecosystem: catalog.EcosystemFabric,
			Name:      "fabric-loader",
			Version:   primary.Version,
		})
	case "forge":
		components = ensureRuntimeComponent(components, catalog.VersionedRef{
			Ecosystem: catalog.EcosystemForge,
			Name:      "forge",
			Version:   primary.Version,
		})
	case "neoforge":
		components = ensureRuntimeComponent(components, catalog.VersionedRef{
			Ecosystem: catalog.EcosystemNeoForge,
			Name:      "neoforge",
			Version:   primary.Version,
		})
	}
	if !hasRuntimeComponent(components, catalog.EcosystemMinecraft, "minecraft") {
		components = ensureRuntimeComponent(components, catalog.VersionedRef{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      "minecraft",
			Version:   identity.MinecraftVersion(),
		})
	}
	return components
}

func ensureRuntimeComponent(
	components []catalog.VersionedRef,
	component catalog.VersionedRef,
) []catalog.VersionedRef {
	if component.Version == "" || component.Version == versionpkg.Unknown {
		return components
	}
	if hasRuntimeComponent(components, component.Ecosystem, component.Name.String()) {
		return components
	}
	return append(components, component)
}

func hasRuntimeComponent(
	components []catalog.VersionedRef,
	ecosystem catalog.Ecosystem,
	name string,
) bool {
	for _, component := range components {
		if component.Ecosystem == ecosystem && component.Name == catalog.Name(name) {
			return true
		}
	}
	return false
}
