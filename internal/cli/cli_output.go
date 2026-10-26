package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/telemetry"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

// installOutput adapts operation traces and results to the command presenter.
type installOutput struct {
	*Output

	phase           string
	resolved        int
	pending         int
	rejected        int
	rejectedProject string
	fetchedBase     int64
	fetchedCurrent  int64
}

func newInstallOutput(cmd *cobra.Command) *installOutput {
	output := NewOperationOutput(cmd)
	output.Progress()
	return &installOutput{Output: output}
}

// Trace returns the hooks the operation reports through.
func (o *installOutput) Trace() *install.Trace {
	return &install.Trace{
		PhaseStarted:  o.phaseStarted,
		PhaseDone:     o.phaseDone,
		FetchStarted:  o.fetchStarted,
		FetchProgress: o.fetchProgress,
		FetchDone:     o.fetchDone,
		ProcessOutput: processLog{o},
		Resolve: resolve.Trace{
			State:    o.resolveState,
			Rejected: o.resolveRejected,
		},
	}
}

func (o *installOutput) phaseStarted(name string) {
	o.phase = name
	o.resolved = 0
	o.pending = 0
	o.rejected = 0
	o.rejectedProject = ""
	o.renderer.PhaseStarted(phaseLabel(name))
}

func (o *installOutput) phaseDone(phase install.PhaseInfo) {
	o.phase = ""
	text := phaseText(phase)
	if text == "" {
		return
	}
	o.renderer.PhaseDone(text, phase.Elapsed)
}

func (o *installOutput) fetchStarted(name string, size int64) {
	o.fetchedCurrent = 0
	o.renderer.Download(name, 0, size)
	o.updateStatus()
}

func (o *installOutput) fetchProgress(name string, read, total int64) {
	o.fetchedCurrent = read
	o.renderer.Download(name, read, total)
	o.updateStatus()
}

func (o *installOutput) fetchDone(string, bool) {
	o.fetchedBase += o.fetchedCurrent
	o.fetchedCurrent = 0
	o.renderer.DownloadDone()
	o.updateStatus()
}

func (o *installOutput) resolveState(resolved, pending int) {
	o.resolved = resolved
	o.pending = pending
	o.updateStatus()
}

func (o *installOutput) resolveRejected(project, _, _ string) {
	o.rejected++
	o.rejectedProject = project
	o.updateStatus()
}

func (o *installOutput) updateStatus() {
	if o.phase == "" {
		return
	}
	o.renderer.Status(o.statusText())
}

func (o *installOutput) statusText() string {
	var parts []string
	if o.phase == "packages" {
		parts = append(
			parts,
			fmt.Sprintf("%d resolved", o.resolved),
			fmt.Sprintf("%d pending", o.pending),
		)
	}
	if fetched := o.fetchedBase + o.fetchedCurrent; fetched > 0 {
		parts = append(parts, style.FormatBytesDecimal(fetched)+" fetched")
	}
	if o.rejectedProject != "" {
		parts = append(parts, fmt.Sprintf("%s: %d builds rejected", o.rejectedProject, o.rejected))
	}
	return strings.Join(parts, " · ")
}

// Render prints the result. Human output goes to stderr; JSON goes to stdout.
func (o *installOutput) Render(result *install.Result) error {
	if result == nil {
		return nil
	}
	telemetry.ObserveServer(
		result.Server.Product.String(),
		result.Server.Minecraft.String(),
	)
	result.Warnings = append(result.Warnings, javaWarnings(result.Java)...)
	result.WarningMessages = warningMessages(result.Warnings)
	if o.JSON() {
		return o.WriteJSON(result)
	}
	if !result.Changed() {
		if !result.PublishedManifest && !result.PublishedLock {
			line := style.Success("✓") + " Already up to date"
			if result.TotalPackages > 0 {
				line += style.Muted(fmt.Sprintf(" · %d packages", result.TotalPackages))
			}
			o.renderer.Println(line)
		}
	} else {
		var changes []string
		if result.Server.Change == install.ChangeUpgraded {
			changes = append(changes, serverChangeLine(result.Server))
		}
		changes = append(changes, packageChangeLines(result.Packages)...)
		if len(changes) > 0 {
			o.renderer.Println(append([]string{""}, changes...)...)
		}
	}
	if trailer := publicationLine(result); trailer != "" {
		o.renderer.Println("", trailer)
	}
	// Flush the summary before warnings so the two surfaces cannot interleave.
	o.renderer.Close()
	for _, warning := range result.Warnings {
		o.Warn(warning)
	}
	for _, diagnostic := range result.Diagnostics {
		o.Warn(fmt.Errorf("%s: %s", diagnostic.Path, diagnostic.Message))
	}
	return nil
}

// Failure converts an operation error into the detail the command framework
// renders. Unknown errors pass through unchanged.
func (o *installOutput) Failure(err error) error {
	if err == nil {
		return nil
	}
	var failure *resolve.Failure
	if errors.As(err, &failure) {
		return resolutionDetail(failure)
	}
	var committed *install.CommittedError
	if errors.As(err, &committed) &&
		(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return &report.Detail{
			Title: "interrupted",
			Lines: []string{
				"The installation was committed, but file provisioning was interrupted.",
				"Run lucy install again to finish.",
			},
			Cause: err,
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &report.Detail{
			Title: "interrupted",
			Lines: []string{"The installation stopped before it committed; staged files were rolled back."},
			Cause: err,
		}
	}
	if errors.Is(err, bootstrap.ErrNoJava) {
		return &report.Detail{
			Title: "no Java runtime",
			Lines: []string{err.Error()},
			Hint:  "Run lucy install --java to install the Java runtime the server asks for.",
			Cause: err,
		}
	}
	return err
}

// processLog forwards subprocess output to the current progress area, which
// changes when a Java installation suspends it.
type processLog struct{ o *installOutput }

func (l processLog) Write(data []byte) (int, error) {
	return l.o.renderer.LogWriter().Write(data)
}

func warningMessages(warnings []error) []string {
	messages := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning != nil {
			messages = append(messages, warning.Error())
		}
	}
	return messages
}

func phaseLabel(name string) string {
	switch name {
	case "server":
		return "Resolving server"
	case "packages":
		return "Resolving packages"
	case "python environment":
		return "Preparing Python environment"
	case "install":
		return "Installing"
	default:
		return style.Capitalize(name)
	}
}

func phaseText(phase install.PhaseInfo) string {
	switch phase.Name {
	case "server":
		if phase.Version == "" {
			return fmt.Sprintf("Resolved %s", phase.Product)
		}
		return fmt.Sprintf(
			"Resolved %s %s for Minecraft %s",
			phase.Product,
			phase.Version,
			phase.Minecraft,
		)
	case "packages":
		if phase.Count == 0 && phase.Total == 0 {
			return ""
		}
		if phase.Total > 0 && phase.Count > phase.Total {
			return fmt.Sprintf(
				"Resolved %d packages (%d requested, %d dependencies)",
				phase.Count,
				phase.Total,
				phase.Count-phase.Total,
			)
		}
		return fmt.Sprintf("Resolved %d packages", phase.Count)
	case "python environment":
		return "Prepared Python environment"
	case "install":
		if phase.Count == 0 {
			return ""
		}
		if phase.Count == 1 {
			return "Installed 1 file"
		}
		return fmt.Sprintf("Installed %d files", phase.Count)
	default:
		return style.Capitalize(phase.Name)
	}
}

func serverChangeLine(change install.ServerChange) string {
	version := string(change.Version)
	if change.Change == install.ChangeUpgraded {
		version = fmt.Sprintf("%s → %s", change.Previous, change.Version)
	}
	return fmt.Sprintf(
		"  %s %s %s",
		changeGlyph(change.Change),
		change.Product,
		version,
	)
}

func packageChangeLines(changes []install.PackageChange) []string {
	width := 0
	for _, change := range changes {
		width = max(width, len(change.Name))
	}
	lines := make([]string, 0, len(changes))
	for _, change := range changes {
		version := string(change.Version)
		switch change.Change {
		case install.ChangeUpgraded, install.ChangeDowngraded, install.ChangeChanged:
			version = fmt.Sprintf("%s → %s", change.Previous, change.Version)
		case install.ChangeRebuilt:
			if change.PreviousReleaseID != "" && change.ReleaseID != "" && change.PreviousReleaseID != change.ReleaseID {
				version = fmt.Sprintf("%s (build %s → %s)", change.Version, change.PreviousReleaseID, change.ReleaseID)
			} else {
				version = fmt.Sprintf("%s (build changed)", change.Version)
			}
		}
		line := fmt.Sprintf(
			"  %s %-*s  %s",
			changeGlyph(change.Change),
			width,
			change.Name,
			version,
		)
		if change.Change == install.ChangeAdded && !change.Direct && len(change.RequiredBy) > 0 {
			line += "   " + style.Muted("dependency of "+joinLimited(change.RequiredBy, 2))
		}
		lines = append(lines, line)
	}
	return lines
}

func changeGlyph(change install.Change) string {
	switch change {
	case install.ChangeAdded:
		return style.Success("+")
	case install.ChangeRemoved:
		return style.Failure("-")
	case install.ChangeUpgraded:
		return style.Warning("~")
	case install.ChangeDowngraded:
		return style.Failure("v")
	case install.ChangeRebuilt:
		return style.Warning("r")
	case install.ChangeChanged:
		return style.Warning("!")
	default:
		return " "
	}
}

func joinLimited(values []string, limit int) string {
	if len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(values[:limit], ", "), len(values)-limit)
}

func pluralBuilds(count int) string {
	if count == 1 {
		return "1 build"
	}
	return fmt.Sprintf("%d builds", count)
}

func publicationLine(result *install.Result) string {
	switch {
	case result.PublishedManifest && result.PublishedLock:
		return style.Muted("Updated lucy.yaml and lucy-lock.yaml")
	case result.PublishedManifest:
		return style.Muted("Updated lucy.yaml")
	case result.PublishedLock:
		return style.Muted("Updated lucy-lock.yaml")
	default:
		return ""
	}
}

func resolutionDetail(failure *resolve.Failure) *report.Detail {
	detail := &report.Detail{Cause: failure}
	if failure.Aborted {
		detail.Title = failure.Error()
		if failure.Project != "" {
			detail.Lines = append(detail.Lines, fmt.Sprintf(
				"%s rejected %d builds for Minecraft %s on %s before the search stopped",
				failure.Project,
				failure.Candidates,
				failure.Minecraft,
				failure.Ecosystem,
			))
			appendRejectionGroups(detail, failure)
		}
		return detail
	}
	versionOnly := len(failure.Groups) == 1 &&
		failure.Groups[0].Kind == resolve.RejectionVersionMismatch
	if versionOnly {
		group := failure.Groups[0]
		detail.Title = fmt.Sprintf("%s has no build matching %q", failure.Project, group.Detail)
		if failure.Candidates > 0 {
			detail.Lines = append(detail.Lines, fmt.Sprintf(
				"%d builds exist for Minecraft %s on %s",
				failure.Candidates,
				failure.Minecraft,
				failure.Ecosystem,
			))
		}
		if group.FirstVersion != "" && group.LastVersion != "" && group.FirstVersion != group.LastVersion {
			detail.Lines = append(detail.Lines, group.FirstVersion+" … "+group.LastVersion)
		}
		if group.FirstVersion != "" {
			detail.Hint = fmt.Sprintf("lucy add %s@%s", failure.Project, group.FirstVersion)
		}
		return detail
	}
	detail.Title = fmt.Sprintf("no build of %s works on this server", failure.Project)
	if failure.Candidates > 0 {
		detail.Lines = append(detail.Lines, fmt.Sprintf(
			"checked %d builds for Minecraft %s on %s",
			failure.Candidates,
			failure.Minecraft,
			failure.Ecosystem,
		))
	}
	appendRejectionGroups(detail, failure)
	return detail
}

// appendRejectionGroups reports each aggregated rejection on its own line.
func appendRejectionGroups(detail *report.Detail, failure *resolve.Failure) {
	for _, group := range failure.Groups {
		switch group.Kind {
		case resolve.RejectionEmpty:
			detail.Lines = append(detail.Lines, group.Detail)
		case resolve.RejectionModules:
			prefix := pluralBuilds(group.Count)
			if group.Count == failure.Candidates {
				prefix = fmt.Sprintf("all %d", group.Count)
			}
			detail.Lines = append(detail.Lines, prefix+": "+group.Detail)
		default:
			detail.Lines = append(detail.Lines, pluralBuilds(group.Count)+": "+group.Detail)
		}
	}
}
