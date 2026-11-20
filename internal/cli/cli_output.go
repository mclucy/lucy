package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/terminal/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

// installJSON is the machine-readable result shared by every operation that
// resolves, publishes, and installs: add, remove, install, init, and upgrade.
var installJSON JSONResult[*install.Result]

// installOutput adapts operation traces and results to the command presenter.
type installOutput struct {
	*Output
	input io.Reader

	spool *log.ProcessSpool

	note  string
	noted bool

	phase           string
	phaseCount      int
	phaseTotal      int
	resolved        int
	pending         int
	rejected        int
	rejectedProject string
	fetchedBytes    int64
	// activeFetches keeps received bytes per transfer name. Archive workers
	// may start and finish several transfers interleaved, so one current
	// transfer cannot attribute the bytes that end. Transfers that share a
	// name use a FIFO slot each; a producer that passes the logical artifact
	// path gives every slot a distinct key and exact accounting.
	activeFetches map[string][]int64
	fetchedFiles  int
}

func newInstallOutput(cmd *cobra.Command, inv *Invocation) *installOutput {
	output := NewOperationOutput(cmd, inv)
	output.Progress()
	return &installOutput{
		Output: output,
		input:  cmd.InOrStdin(),
		spool:  log.NewProcessSpool(cmd.Name(), inv.Logger),
	}
}

// Trace returns the hooks the operation reports through.
func (o *installOutput) Trace() *install.Trace {
	trace := &install.Trace{
		PhaseStarted:  o.phaseStarted,
		PhaseProgress: o.phaseProgress,
		PhaseDone:     o.phaseDone,
		FetchStarted:  o.fetchStarted,
		FetchProgress: o.fetchProgress,
		FetchDone:     o.fetchDone,
		ProcessOutput: processTee{area: processLog{o}, spool: o.spool},
		Resolve: resolve.Trace{
			State:    o.resolveState,
			Rejected: o.resolveRejected,
		},
	}
	if o.inv.TelemetryObservation != nil {
		trace.InputScale = o.inv.TelemetryObservation.ObserveInputScale
	}
	return trace
}

func (o *installOutput) phaseStarted(name string) {
	o.inv.TelemetryObservation.PhaseStart(name)
	o.phase = name
	o.phaseCount = 0
	o.phaseTotal = 0
	o.resolved = 0
	o.pending = 0
	o.rejected = 0
	o.rejectedProject = ""
	o.fetchedFiles = 0
	o.renderer.PhaseStarted(phaseLabel(name))
}

// phaseProgress reports bounded work inside the active phase. A late update
// from a phase that already ended is dropped, so a status line can never
// outlive the completion that replaced it.
func (o *installOutput) phaseProgress(name string, count, total int) {
	if name != o.phase {
		return
	}
	o.phaseCount = count
	o.phaseTotal = total
	o.updateStatus()
}

func (o *installOutput) phaseDone(phase install.PhaseInfo) {
	o.inv.TelemetryObservation.PhaseDone(phase.Name, phase.Elapsed)
	o.inv.TelemetryObservation.ObservePhaseCount(phase.Name, phase.Count, phase.Total)
	o.phase = ""
	o.phaseCount = 0
	o.phaseTotal = 0
	// An empty text ends the phase without printing: a phase whose outcome
	// has no prose must still clear the live area.
	o.renderer.PhaseDone(phaseText(phase), phase.Elapsed)
}

func (o *installOutput) fetchStarted(name string, size int64) {
	if o.activeFetches == nil {
		o.activeFetches = map[string][]int64{}
	}
	o.activeFetches[name] = append(o.activeFetches[name], 0)
	o.renderer.Download(name, 0, size)
	o.updateStatus()
}

func (o *installOutput) fetchProgress(name string, read, total int64) {
	if o.activeFetches == nil {
		o.activeFetches = map[string][]int64{}
	}
	slots := o.activeFetches[name]
	if len(slots) == 0 {
		slots = []int64{read}
	} else {
		slots[len(slots)-1] = read
	}
	o.activeFetches[name] = slots
	o.renderer.Download(name, read, total)
	o.updateStatus()
}

func (o *installOutput) fetchDone(name string, _ bool) {
	o.inv.TelemetryObservation.ObserveFetchDone()
	if slots := o.activeFetches[name]; len(slots) > 0 {
		o.fetchedBytes += slots[0]
		slots = slots[1:]
		if len(slots) == 0 {
			delete(o.activeFetches, name)
		} else {
			o.activeFetches[name] = slots
		}
	}
	o.fetchedFiles++
	o.renderer.DownloadDone()
	o.updateStatus()
}

func (o *installOutput) resolveState(resolved, pending int) {
	o.resolved = resolved
	o.pending = pending
	o.updateStatus()
}

func (o *installOutput) resolveRejected(project, _, _ string) {
	o.inv.TelemetryObservation.ObserveResolveRejected()
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

// activeBytes sums the transfers still running. Completed transfers move into
// fetchedBytes, so interleaved archive workers never double count.
func (o *installOutput) activeBytes() int64 {
	var bytes int64
	for _, slots := range o.activeFetches {
		for _, read := range slots {
			bytes += read
		}
	}
	return bytes
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
	if o.phase == "files" && o.fetchedFiles > 0 {
		label := fmt.Sprintf("%d files", o.fetchedFiles)
		if o.fetchedFiles == 1 {
			label = "1 file"
		}
		parts = append(parts, label)
	}
	if o.phaseTotal > 0 {
		switch o.phase {
		case "classification":
			parts = append(parts, fmt.Sprintf("%d of %d mod files", o.phaseCount, o.phaseTotal))
		case "package staging":
			parts = append(parts, fmt.Sprintf("%d of %d packages", o.phaseCount, o.phaseTotal))
		case "processors":
			parts = append(parts, fmt.Sprintf("%d of %d processors", o.phaseCount, o.phaseTotal))
		}
	}
	if fetched := o.fetchedBytes + o.activeBytes(); fetched > 0 {
		parts = append(parts, style.FormatBytesDecimal(fetched)+" fetched")
	}
	if o.rejectedProject != "" {
		parts = append(parts, fmt.Sprintf("%d builds of %s rejected", o.rejected, o.rejectedProject))
	}
	return strings.Join(parts, ", ")
}

// Render prints the result. Human output goes to stderr; JSON goes to stdout.
func (o *installOutput) Render(result *install.Result) error {
	if result == nil {
		return nil
	}
	result.Warnings = append(result.Warnings, javaWarnings(result.Java)...)
	warnings := warningMessages(result.Warnings)
	// Output.Warn has already counted the warnings collected earlier in the run.
	o.inv.TelemetryObservation.ObserveWarnings(len(warnings) + len(result.Diagnostics))
	result.WarningMessages = append(warnings, o.Warnings()...)
	if o.JSON() {
		return installJSON.Write(o.Output, result)
	}
	if !result.Changed() {
		if !result.PublishedManifest && !result.PublishedLock {
			line := o.inv.Theme.Success("✓") + " Already up to date"
			if result.TotalPackages > 0 {
				line = o.inv.Theme.Success("✓") + " " +
					packageCountLabel(result.TotalPackages) + " already up to date"
			}
			o.renderer.Println(line)
		}
	} else {
		var changes []string
		if result.Server.Change == install.ChangeUpgraded {
			changes = append(changes, serverChangeLine(result.Server, o.inv.Theme))
		}
		changes = append(changes, packageChangeLines(result.Packages, o.inv.Theme)...)
		if len(changes) > 0 {
			o.renderer.Println(append([]string{""}, changes...)...)
		}
	}
	if trailer := publicationLine(result, o.inv.Theme); trailer != "" {
		o.renderer.Println("", trailer)
	}
	// Flush the summary before warnings so the two surfaces cannot interleave.
	o.renderer.Close()
	for _, warning := range result.Warnings {
		o.warn(warning, false)
	}
	for _, diagnostic := range result.Diagnostics {
		o.warn(fmt.Errorf("%s: %s", diagnostic.Path, diagnostic.Message), false)
	}
	return nil
}

// Failure converts an operation error into the detail the command framework
// renders. When a subprocess ran, the retained process log is named once, in
// the returned error and in every detail built from it.
func (o *installOutput) Failure(err error) error {
	if err == nil {
		return nil
	}
	note := o.processLogNote()
	var failure *resolve.Failure
	if errors.As(err, &failure) {
		detail := resolutionDetail(failure)
		detail.Lines = append(detail.Lines, "Resolution did not change the manifest or lock.")
		return detail
	}
	var committed *install.CommittedError
	if errors.As(err, &committed) &&
		(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return &report.Detail{
			Title: "interrupted",
			Lines: detailLines(
				note,
				"The installation was committed, but file provisioning was interrupted.",
				"Run lucy install again to finish.",
			),
			Cause: err,
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &report.Detail{
			Title: "interrupted",
			Lines: detailLines(
				note,
				"The installation stopped before it committed; staged files were rolled back.",
			),
			Cause: err,
		}
	}
	if errors.Is(err, bootstrap.ErrNoJava) {
		return &report.Detail{
			Title: "no Java runtime",
			Lines: detailLines(note, err.Error()),
			Hint:  "Run lucy install --java to install the Java runtime the server asks for.",
			Cause: err,
		}
	}
	if note != "" {
		return fmt.Errorf("%w\n%s", err, note)
	}
	return err
}

// processLogNote retains the process log and returns the sentence that names
// it. It returns an empty string when no subprocess ran, so unrelated
// failures stay unchanged. The first call fixes the sentence, so a presenter
// that adapts the failure again names the same file. A log that dropped output
// says so instead of claiming it saved everything observed.
func (o *installOutput) processLogNote() string {
	if o.noted {
		return o.note
	}
	o.noted = true
	retained, ok := o.spool.Retain()
	if !ok {
		return ""
	}
	observed := style.FormatBytesDecimal(retained.Observed)
	if retained.Truncated {
		o.note = fmt.Sprintf(
			"Subprocess output (%s observed) is incomplete: %s holds %s",
			observed,
			retained.Path,
			style.FormatBytesDecimal(retained.Stored),
		)
		return o.note
	}
	o.note = fmt.Sprintf(
		"Subprocess output (%s) saved to %s",
		observed,
		retained.Path,
	)
	return o.note
}

// Close ends the command's output lifetime. The progress area stops and an
// unretained process log is discarded, so a surviving file is always evidence
// of a failure.
func (o *installOutput) Close() {
	o.Output.Close()
	o.spool.Discard()
}

// EndProgress closes the progress area only. A prompt or plain report owns the
// terminal while the command keeps running, and the process log is kept for
// the steps that follow.
func (o *installOutput) EndProgress() {
	o.Output.Close()
}

// detailLines appends the process log note to the fixed lines of a failure
// detail, so a human rendering and a machine document name the same evidence.
func detailLines(note string, lines ...string) []string {
	if note == "" {
		return lines
	}
	return append(lines, note)
}

// processLog forwards subprocess output to the current progress area, which
// changes when a Java installation suspends it.
type processLog struct{ o *installOutput }

func (l processLog) Write(data []byte) (int, error) {
	return l.o.renderer.LogWriter().Write(data)
}

// processTee streams subprocess output to the progress area and to the
// retained process log. Section markers reach only the log, because the
// command line they name already narrates on the terminal.
type processTee struct {
	area  io.Writer
	spool *log.ProcessSpool
}

func (t processTee) Write(data []byte) (int, error) {
	written, err := t.area.Write(data)
	if err != nil {
		return written, err
	}
	return t.spool.Write(data)
}

func (t processTee) Section(label string) {
	t.spool.Section(label)
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
	case "identification":
		return "Identifying the installed server"
	case "adoption":
		return "Adopting installed packages"
	case "python environment":
		return "Preparing Python environment"
	case "package staging":
		return "Staging packages"
	case "bootstrap snapshot":
		return "Restoring the bootstrap snapshot"
	case "install":
		return "Installing"
	case "download pack":
		return "Downloading the modpack"
	case "download archive":
		return "Downloading the archive"
	case "files":
		return "Writing server files"
	case "classification":
		return "Classifying installed mods"
	case "loader preparation":
		return "Preparing the loader"
	case "processors":
		return "Running installer processors"
	case "loader installer":
		return "Running the loader installer"
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
	case "identification":
		return "Identified the installed server"
	case "classification":
		if phase.Total == 0 {
			return ""
		}
		if phase.Count == phase.Total {
			if phase.Count == 1 {
				return "Kept 1 mod file"
			}
			return fmt.Sprintf("Kept all %d mod files", phase.Count)
		}
		return fmt.Sprintf(
			"Kept %d of %d mod files",
			phase.Count,
			phase.Total,
		)
	case "loader preparation":
		return "Prepared the loader"
	case "bootstrap snapshot":
		return fmt.Sprintf("Restored %d bootstrap files without running the installer", phase.Count)
	case "processors":
		if phase.Count == 0 {
			return ""
		}
		if phase.Total > 0 && phase.Count < phase.Total {
			return fmt.Sprintf(
				"Ran %d of %d installer processors",
				phase.Count,
				phase.Total,
			)
		}
		if phase.Count == 1 {
			return "Ran 1 installer processor"
		}
		return fmt.Sprintf("Ran %d installer processors", phase.Count)
	case "python environment":
		return "Prepared Python environment"
	case "package staging":
		if phase.Count == 0 {
			return ""
		}
		if phase.Total > 0 && phase.Count < phase.Total {
			return fmt.Sprintf(
				"Staged %d of %d packages",
				phase.Count,
				phase.Total,
			)
		}
		if phase.Count == 1 {
			return "Staged 1 package"
		}
		return fmt.Sprintf("Staged %d packages", phase.Count)
	case "download pack":
		return "Downloaded the modpack"
	case "download archive":
		return "Downloaded the archive"
	case "files":
		if phase.Count == 0 {
			return ""
		}
		if phase.Count == 1 {
			return "Wrote 1 server file"
		}
		return fmt.Sprintf("Wrote %d server files", phase.Count)
	case "loader installer":
		return "Ran the loader installer"
	case "adoption":
		if phase.Total == 0 {
			return ""
		}
		if phase.Count == phase.Total {
			if phase.Count == 1 {
				return "Adopted 1 installed file"
			}
			return fmt.Sprintf("Adopted %d installed files", phase.Count)
		}
		return fmt.Sprintf(
			"Adopted %d of %d installed files",
			phase.Count,
			phase.Total,
		)
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

func serverChangeLine(change install.ServerChange, theme style.Theme) string {
	version := string(change.Version)
	if change.Change == install.ChangeUpgraded {
		version = fmt.Sprintf("%s → %s", change.Previous, change.Version)
	}
	return fmt.Sprintf(
		"  %s %s %s",
		changeGlyph(change.Change, theme),
		change.Product,
		version,
	)
}

func packageChangeLines(changes []install.PackageChange, theme style.Theme) []string {
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
			changeGlyph(change.Change, theme),
			width,
			change.Name,
			version,
		)
		if change.Change == install.ChangeAdded && !change.Direct && len(change.RequiredBy) > 0 {
			line += "   " + theme.Muted("dependency of "+joinLimited(change.RequiredBy, 2))
		}
		lines = append(lines, line)
	}
	return lines
}

func changeGlyph(change install.Change, theme style.Theme) string {
	switch change {
	case install.ChangeAdded:
		return theme.Success("+")
	case install.ChangeRemoved:
		return theme.Failure("-")
	case install.ChangeUpgraded:
		return theme.Warning("~")
	case install.ChangeDowngraded:
		return theme.Failure("v")
	case install.ChangeRebuilt:
		return theme.Warning("r")
	case install.ChangeChanged:
		return theme.Warning("!")
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

func packageCountLabel(count int) string {
	if count == 1 {
		return "1 package"
	}
	return fmt.Sprintf("%d packages", count)
}

func publicationLine(result *install.Result, theme style.Theme) string {
	switch {
	case result.PublishedManifest && result.PublishedLock:
		return theme.Muted("Updated lucy.yaml and lucy-lock.yaml")
	case result.PublishedManifest:
		return theme.Muted("Updated lucy.yaml")
	case result.PublishedLock:
		return theme.Muted("Updated lucy-lock.yaml")
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
	if constraint, ok := serverConstraint(failure.Groups); ok {
		detail.Title = fmt.Sprintf(
			"%s requires %s %s, but this server runs %s",
			constraint.RequiredBy,
			constraint.Dependency,
			constraint.Constraint,
			constraint.Selected,
		)
		detail.Lines = append(detail.Lines, fmt.Sprintf(
			"no build of %s for Minecraft %s on %s can change it",
			failure.Project,
			failure.Minecraft,
			failure.Ecosystem,
		))
		appendRejectionGroups(detail, failure)
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

// serverConstraint returns the requirement the server itself fails to satisfy.
// No candidate can change it, so it is the cause of the failure.
func serverConstraint(groups []resolve.RejectionGroup) (resolve.RejectionGroup, bool) {
	for _, group := range groups {
		if group.Kind == resolve.RejectionServer {
			return group, true
		}
	}
	return resolve.RejectionGroup{}, false
}

// appendRejectionGroups reports each aggregated rejection on its own line.
func appendRejectionGroups(detail *report.Detail, failure *resolve.Failure) {
	for _, group := range failure.Groups {
		switch group.Kind {
		case resolve.RejectionEmpty:
			detail.Lines = append(detail.Lines, group.Detail)
		case resolve.RejectionModules, resolve.RejectionServer:
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
