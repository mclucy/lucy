package info

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <package|file>",
	Short: help.InfoShort,
	Long:  help.InfoLong,
	Args:  cobra.ExactArgs(1),
	ValidArgsFunction: func(
		cmd *cobra.Command,
		args []string,
		toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 1 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		candidates, _ := cli.CompletePackageIDSuggestions(
			context.Background(),
			"info",
			toComplete,
		)
		return candidates, cobra.ShellCompDirectiveDefault
	},

	RunE: actionInfo,
}

func NewCommand() *cobra.Command {
	cli.AddLongFlag(infoCmd)
	return infoCmd
}

func actionInfo(cmd *cobra.Command, args []string) error {
	ctx, stop := cli.CommandContext(cmd)
	defer stop()

	output := cli.NewQueryOutput(cmd)
	long, _ := cmd.Flags().GetBool(cli.FlagLong)

	root, err := infoWorkingDirectory()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return infoCancellation(err)
	}

	isArtifact, err := isArtifactPath(root, args[0])
	if err != nil {
		return err
	}
	if isArtifact {
		return actionArtifactInfo(ctx, output, long, root, args[0])
	}

	request, err := input.Parse(args[0])
	if err != nil {
		return infoReferenceFailure(args[0], err)
	}
	request, err = resolveAgainstLock(root, request)
	if err != nil {
		return err
	}
	return actionProjectInfo(ctx, output, long, root, request)
}

// resolveAgainstLock expands a bare package name into the explicit
// "provider:project" reference the current lock binds it to. An explicit
// provider is always honored, and the requested version survives the
// expansion.
func resolveAgainstLock(
	root string,
	request catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	if request.Provider != catalog.ProviderUnspecified {
		return request, nil
	}

	reference, err := cli.ResolveLockReference(root, request.Name.String())
	if err != nil {
		return catalog.VersionedRef{}, infoAmbiguousReferenceFailure(
			request.Name.String(),
			err,
		)
	}
	if reference == "" {
		return request, nil
	}

	resolved, err := input.Parse(reference)
	if err != nil {
		return request, nil
	}
	request.Provider = resolved.Provider
	request.Name = resolved.Name
	return request, nil
}

type infoFetchResult struct {
	info           upstream.Info
	providerErrors []routing.ProviderError
	err            error
}

func actionProjectInfo(
	ctx context.Context,
	output *cli.Output,
	long bool,
	root string,
	request catalog.VersionedRef,
) error {
	providers, err := routing.ResolveInfoProviders(
		request.Ecosystem,
		request.Provider,
	)
	if err != nil {
		return infoProviderSelectionFailure(request, err)
	}

	result, awaitErr := infoAwait(ctx, func() infoFetchResult {
		info, providerErrors, fetchErr := routing.GetInfoFirstSuccess(
			providers,
			request.Ref,
		)
		return infoFetchResult{
			info:           info,
			providerErrors: providerErrors,
			err:            fetchErr,
		}
	})
	if awaitErr != nil {
		if failure := infoOperationFailure(ctx, awaitErr); failure != nil {
			return failure
		}
		return awaitErr
	}
	if result.err != nil {
		if failure := infoOperationFailure(ctx, result.err); failure != nil {
			return failure
		}
		return infoProviderFailure(request, result.providerErrors, result.err)
	}
	if failure := infoOperationFailure(ctx, ctx.Err()); failure != nil {
		return failure
	}

	warnings := infoProviderWarnings(result.providerErrors)
	observed := infoObserveWorkspace(root)
	lock := infoReadLock(root)

	resolvedVersion, versionWarnings, err := infoResolveProjectVersion(
		ctx,
		request,
		result.info.Ref.Provider,
		observed,
		lock,
	)
	if err != nil {
		return err
	}
	warnings = append(warnings, versionWarnings...)

	entry := infoProjectEntry(
		result.info.Metadata,
		result.info.Ref,
		resolvedVersion,
	)
	entry.Installed = infoInstalledAt(
		root,
		request.Ref,
		observed,
		lock,
	)

	document := newInfoDocument(infoKindProject, "")
	document.Entries = append(document.Entries, entry)
	return infoWriteDocument(ctx, output, document, warnings, long)
}

// infoProviderWarnings reports partial provider failures. A successful
// document still carries every partial failure; none of them are written to
// stderr directly.
func infoProviderWarnings(
	providerErrors []routing.ProviderError,
) []string {
	warnings := make([]string, 0, len(providerErrors)+1)
	notFound := make([]string, 0, len(providerErrors))
	for _, providerErr := range providerErrors {
		if infoIsNotFound(providerErr.Err) {
			notFound = append(notFound, providerErr.Provider.Title())
			continue
		}
		warnings = append(warnings, fmt.Sprintf(
			"information from %s failed: %v",
			providerErr.Provider.Title(),
			providerErr.Err,
		))
	}
	sort.Strings(warnings)
	sort.Strings(notFound)
	if len(notFound) > 0 {
		warnings = append(
			warnings,
			"not found on "+strings.Join(notFound, ", "),
		)
	}
	return warnings
}

func infoProviderFailure(
	request catalog.VersionedRef,
	providerErrors []routing.ProviderError,
	err error,
) error {
	name := infoCleanLine(request.Name.String())
	if name == "" {
		name = "the requested project"
	}
	lines := make([]string, 0, len(providerErrors))
	allNotFound := len(providerErrors) > 0
	for _, providerErr := range providerErrors {
		if !infoIsNotFound(providerErr.Err) {
			allNotFound = false
		}
		lines = append(lines, infoCleanLine(fmt.Sprintf(
			"%s: %v",
			providerErr.Provider.Title(),
			providerErr.Err,
		)))
	}
	if len(lines) == 0 {
		lines = append(lines, infoCleanLine(err.Error()))
	}
	sort.Strings(lines)

	code := infoErrorProvider
	title := "cannot retrieve information for " + name
	hint := "Check the project reference, or pass provider:project to choose a source."
	if allNotFound {
		code = infoErrorNotFound
		title = "project not found: " + name
		hint = "Check the project name, or pass provider:project to choose a source."
	}
	return &report.Detail{
		Title: title,
		Lines: lines,
		Hint:  hint,
		Code:  code,
		Cause: err,
	}
}

func infoProviderSelectionFailure(
	request catalog.VersionedRef,
	err error,
) error {
	requested := "automatic"
	if infoProviderUsable(request.Provider) {
		requested = request.Provider.String()
	}
	return &report.Detail{
		Title: "cannot select an information source",
		Lines: []string{
			infoCleanLine(err.Error()),
			"Requested provider: " + requested,
		},
		Hint:  "Pass provider:project to choose a source, for example modrinth:project.",
		Code:  infoErrorProviderPick,
		Cause: err,
	}
}

func infoReferenceFailure(reference string, err error) error {
	return &report.Detail{
		Title: "cannot parse package reference",
		Lines: []string{
			infoCleanLine(reference),
			infoCleanLine(err.Error()),
		},
		Hint:  "Use provider:project@version, for example modrinth:fabric-api.",
		Code:  infoErrorReference,
		Cause: err,
	}
}

func infoAmbiguousReferenceFailure(name string, err error) error {
	return &report.Detail{
		Title: "cannot resolve " + infoCleanLine(name) + " in the current lock",
		Lines: []string{infoCleanLine(err.Error())},
		Hint:  "Pass an explicit provider:project reference.",
		Code:  infoErrorAmbiguous,
		Cause: err,
	}
}

func infoIsNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}
