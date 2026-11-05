package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

// reportObservationFindings renders each consumed observation once.
func reportObservationFindings(observed *workspace.Workspace, output *Output) {
	if observed == nil {
		return
	}
	for _, warning := range observed.Warnings {
		output.Warn(warning)
	}
	reportDiscoveryFindings(&observed.Discovery, output)
}

// reportDiscoveryFindings presents one discovery result. A resolved layout
// never reports a failure: automatic choices are warnings, and unmanaged or
// alternative entry points stay informational.
func reportDiscoveryFindings(observed *discovery.Result, output *Output) {
	if observed == nil {
		return
	}
	selection := observed.Selection
	resolved := selection.IsValid()
	automatic := selection.Source == discovery.SourcePreferred
	unresolvedReported := false
	unreadableReported := map[string]bool{}
	for finding := range observed.Findings() {
		switch finding.Kind {
		case discovery.FindingAutomaticSelection:
			output.Warn(automaticSelectionMessage(observed, finding))
		case discovery.FindingUnreadableFile:
			key := unreadableFindingKey(finding)
			if unreadableReported[key] {
				continue
			}
			unreadableReported[key] = true
			output.Warn(unreadableFindingError(finding))
		case discovery.FindingAlternativeEntryPoint,
			discovery.FindingUnmanagedInstallation:
			message := entryPointFindingMessage(finding)
			if output.JSON() {
				output.Warn(errors.New(message))
			} else {
				output.Info(message)
			}
		case discovery.FindingMissingDeclaredInstallation:
			output.Warn(errors.New(
				"declared server installation is missing; run lucy install --locked",
			))
		case discovery.FindingOwnershipError:
			output.Warn(fmt.Errorf(
				"cannot verify ownership of %s: %w",
				finding.Path,
				finding.Cause,
			))
		case discovery.FindingModifiedFile:
			output.Warn(fmt.Errorf(
				"managed file %s was modified; it stays unmanaged",
				finding.Path,
			))
		case discovery.FindingReceiptError:
			if finding.Cause != nil {
				output.Warn(fmt.Errorf(
					"cannot verify installation ownership: %w",
					finding.Cause,
				))
			} else {
				output.Warn(errors.New("cannot verify installation ownership"))
			}
		case discovery.FindingInvalidIdentity:
			output.Warn(fmt.Errorf(
				"cannot identify the server in %s: %v",
				finding.Path,
				finding.Cause,
			))
		case discovery.FindingConflictingIdentities,
			discovery.FindingAmbiguousInstallations:
			if resolved || automatic || unresolvedReported {
				continue
			}
			message := unresolvedRuntimeMessage(observed, finding)
			if output.JSON() {
				output.Warn(message)
			} else {
				output.Error(message)
			}
			unresolvedReported = true
		}
	}
}

// unreadableFindingKey identifies one underlying read failure. Recognizers
// can inspect the same artifact, so identical path and cause pairs collapse.
func unreadableFindingKey(finding discovery.Finding) string {
	cause := ""
	if finding.Cause != nil {
		cause = finding.Cause.Error()
	}
	return finding.Path + "\x00" + cause
}

func unreadableFindingError(finding discovery.Finding) error {
	if finding.Cause != nil {
		return fmt.Errorf("cannot read %s: %w", finding.Path, finding.Cause)
	}
	return fmt.Errorf("cannot read %s", finding.Path)
}

// unreadableFindingLines presents each underlying read failure once for a
// command failure detail.
func unreadableFindingLines(result *discovery.Result) []string {
	if result == nil {
		return nil
	}
	var lines []string
	seen := map[string]bool{}
	for _, finding := range result.Diagnostics {
		if finding.Kind != discovery.FindingUnreadableFile {
			continue
		}
		key := unreadableFindingKey(finding)
		if seen[key] {
			continue
		}
		seen[key] = true
		if finding.Cause != nil {
			lines = append(
				lines,
				fmt.Sprintf("%s: %v", finding.Path, finding.Cause),
			)
		} else {
			lines = append(lines, finding.Path)
		}
	}
	return lines
}

func entryPointFindingMessage(finding discovery.Finding) string {
	label := "unmanaged entry point"
	if finding.Kind == discovery.FindingUnmanagedInstallation {
		label = "unmanaged server entry point"
	}
	return fmt.Sprintf("%s %s; left unchanged", label, finding.Path)
}

// automaticSelectionMessage names the chosen entry point and every competing
// entry point. The choice is read-only; the message suggests recording it.
func automaticSelectionMessage(
	observed *discovery.Result,
	finding discovery.Finding,
) error {
	selected := finding.Path
	if selected == "" && observed.Selection.EntryPoint != nil {
		selected = observed.Selection.EntryPoint.Path
	}
	competitors := finding.Paths
	if len(competitors) == 0 {
		competitors = competingEntryPoints(observed, selected)
	}
	message := fmt.Sprintf(
		"multiple server runtimes found; using %s automatically",
		selected,
	)
	if len(competitors) > 0 {
		message += "; competing entry points: " + strings.Join(competitors, ", ")
	}
	message += "; run lucy init --entry-point '<path>' to record a choice"
	return errors.New(message)
}

func unresolvedRuntimeMessage(
	observed *discovery.Result,
	finding discovery.Finding,
) error {
	paths := finding.Paths
	if len(paths) == 0 {
		paths = competingEntryPoints(observed, "")
	}
	if len(paths) == 0 {
		return errors.New("no unambiguous server runtime was found")
	}
	commands := make([]string, 0, len(paths))
	for _, path := range paths {
		commands = append(
			commands,
			"lucy init --entry-point '"+strings.ReplaceAll(path, "'", "'\\''")+"'",
		)
	}
	return fmt.Errorf(
		"cannot choose between server runtimes; select one with %s",
		strings.Join(commands, " or "),
	)
}

// competingEntryPoints is a fallback for findings that carry no paths. It
// lists every entry point except the selected one in a stable order.
func competingEntryPoints(
	result *discovery.Result,
	selected string,
) []string {
	var paths []string
	seen := map[string]bool{}
	for i := range result.Installations {
		for file := range result.Installations[i].EntryPoints() {
			if file.Path == "" || file.Path == selected || seen[file.Path] {
				continue
			}
			seen[file.Path] = true
			paths = append(paths, file.Path)
		}
	}
	slices.Sort(paths)
	return paths
}
