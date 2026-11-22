package cmd

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/internal/agent"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/telemetry"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/terminal/report"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type telemetryInvocation struct {
	Intent    string              `json:"intent"`
	Arguments []telemetryArgument `json:"arguments,omitempty"`
}

// beginTelemetryObservation starts the bounded observation only after the
// existing policy has enabled telemetry for this invocation. The observer is
// carried by the command context so prompt helpers can record their own wait.
func (r *Runner) beginTelemetryObservation(cmd *cobra.Command) {
	if !r.invocation.TelemetryEnabled || r.invocation.TelemetryObservation != nil {
		return
	}
	observation := cli.NewTelemetryObservation()
	observation.AttachLogger(r.invocation.Logger)
	r.invocation.TelemetryObservation = observation
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cmd.SetContext(cli.WithTelemetryObservation(ctx, observation))
	observation.PhaseStart("setup")
	r.setupStarted = time.Now()
}

// completeSetupObservation closes the setup phase that covers service
// wiring. A failure leaves it active, which preserves the unfinished stage.
func (r *Runner) completeSetupObservation() {
	observation := r.invocation.TelemetryObservation
	if observation == nil || r.setupStarted.IsZero() {
		return
	}
	observation.PhaseDone("setup", time.Since(r.setupStarted))
	r.setupStarted = time.Time{}
}

// startCommandObservation begins the command parent phase immediately before
// the command body runs. Subphases may overlap it.
func (r *Runner) startCommandObservation() {
	observation := r.invocation.TelemetryObservation
	if observation == nil || !r.setupStarted.IsZero() {
		return
	}
	observation.PhaseStart("command")
	r.commandStarted = time.Now()
}

// completeCommandObservation closes the command phase only for a successful
// invocation. Failed and canceled traces keep the unfinished phase visible.
func (r *Runner) completeCommandObservation(ended time.Time, commandErr error) {
	observation := r.invocation.TelemetryObservation
	if observation == nil || r.commandStarted.IsZero() || commandErr != nil {
		return
	}
	observation.PhaseDone("command", ended.Sub(r.commandStarted))
}

// telemetryFailureKind classifies a failed invocation without retaining its
// text. Usage is decided once by the caller so report and context agree.
func (r *Runner) telemetryFailureKind(commandErr error, usageError bool) string {
	if commandErr == nil {
		return ""
	}
	if usageError {
		return "usage"
	}
	if _, panicked := errors.AsType[*panicError](commandErr); panicked {
		return "panic"
	}
	if _, handled := errors.AsType[*cli.ExitError](commandErr); handled {
		return "handled"
	}
	return "command"
}

// telemetryUsageError recognizes framework rejections and structured usage
// documents, including schema's invalid command path and unsupported output.
func (r *Runner) telemetryUsageError(err error) bool {
	if r.flagParsingFailed || cli.IsUsageError(err) {
		return true
	}
	detail, ok := errors.AsType[*report.Detail](err)
	return ok && detail != nil && telemetryUsageCode(detail.Code)
}

func telemetryUsageCode(code string) bool {
	return code == cli.CodeInvalidInvocation ||
		code == "usage.unsupported_output" ||
		strings.HasPrefix(code, "usage.")
}

// telemetryCancelReason distinguishes causes only when the boundary knows
// them. A typed signal cause is required before calling a cancellation an OS
// interruption; generic cancellation stays generic. A prompt abort keeps its
// identity because cli.ErrPromptAborted also matches context.Canceled.
func (r *Runner) telemetryCancelReason(commandErr error) string {
	if commandErr == nil {
		return ""
	}
	if !errors.Is(commandErr, context.Canceled) &&
		!errors.Is(commandErr, context.DeadlineExceeded) {
		return ""
	}
	if errors.Is(commandErr, cli.ErrPromptAborted) {
		return "prompt_aborted"
	}
	if errors.Is(commandErr, context.DeadlineExceeded) ||
		(r.runContext != nil && errors.Is(r.runContext.Err(), context.DeadlineExceeded)) {
		return "deadline"
	}
	if telemetrySignalCanceled(r.runContext) {
		return "interrupted"
	}
	return "context_canceled"
}

type telemetrySignalCause interface {
	SignalCancellation()
}

func telemetrySignalCanceled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	cause := context.Cause(ctx)
	if cause == nil {
		return false
	}
	var signal telemetrySignalCause
	return errors.As(cause, &signal)
}

// telemetryFailureCode maps typed failure causes to the fixed event codes used
// by durable aggregation. An explicit report.Detail code is applied later and
// keeps precedence.
func telemetryFailureCode(err error) string {
	if err == nil {
		return ""
	}
	if selection, ok := errors.AsType[*discovery.SelectionError](err); ok && selection != nil {
		switch selection.State {
		case discovery.SelectionAmbiguous:
			return "workspace.selection_ambiguous"
		case discovery.SelectionUnmatched:
			return "workspace.selection_unmatched"
		case discovery.SelectionMissingDeclared:
			return "workspace.missing_declared"
		case discovery.SelectionInvalid, discovery.SelectionNone:
			return "workspace.no_launchable_server"
		}
	}
	if errors.Is(err, fsutil.ErrLocked) {
		return "workspace.lock_busy"
	}
	if errors.Is(err, bootstrap.ErrNoJava) {
		return "runtime.no_java"
	}
	if _, ok := errors.AsType[*resolve.SearchLimitError](err); ok {
		return "resolve.search_limit"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "io.permission_denied"
	}
	return ""
}

func (r *Runner) invocationContext(command *cobra.Command, usageError bool) telemetry.Context {
	intent := "command"
	if usageError {
		intent = "invalid_usage"
	}
	data := telemetryInvocation{Intent: intent}
	if command != nil && command.Flags().Parsed() && !r.flagParsingFailed {
		data.Arguments = telemetryArguments(command)
	}
	return telemetry.Context{
		Kind:   "invocation",
		Status: telemetry.ContextPresent,
		Data:   data,
	}
}

func telemetryFlagSource(command *cobra.Command, name string) *pflag.Flag {
	if command == nil {
		return nil
	}
	if flag := command.Flags().Lookup(name); flag != nil {
		return flag
	}
	if flag := command.InheritedFlags().Lookup(name); flag != nil {
		return flag
	}
	if root := command.Root(); root != nil {
		return root.PersistentFlags().Lookup(name)
	}
	return nil
}

func telemetryBoolFlag(command *cobra.Command, name string) bool {
	flag := telemetryFlagSource(command, name)
	if flag == nil {
		return false
	}
	value, err := strconv.ParseBool(flag.Value.String())
	return err == nil && value
}

// buildTelemetryContexts assembles the bounded envelope. Contexts the command
// did not observe are explicit, and observation gaps are distinct from a
// failed observation.
func (r *Runner) buildTelemetryContexts(
	command *cobra.Command,
	usageError bool,
	failureKind, cancelReason string,
	ended time.Time,
) []telemetry.Context {
	observation := r.invocation.TelemetryObservation
	contexts := make([]telemetry.Context, 0, 8)
	contexts = append(contexts, r.invocationContext(command, usageError))
	if observation != nil {
		if cancelReason != "" {
			observation.SetCancelReason(cancelReason)
		}
		if failureKind != "" && cancelReason == "" {
			observation.SetFailureKind(failureKind)
		}
		r.invocation.MergeWorkspacePackages(observation.Packages())
		contexts = append(contexts, observation.Execution(ended))
	} else {
		contexts = append(contexts, telemetry.Context{
			Kind:   telemetry.ContextExecution,
			Status: telemetry.ContextAbsent,
			Reason: telemetry.ContextReasonNotReached,
		})
	}

	seen := map[telemetry.ContextKind]bool{}
	for _, observed := range r.invocation.TelemetryContexts {
		contexts = append(contexts, normalizeObservedContext(observed))
		seen[observed.Kind] = true
	}
	gapReason := telemetry.ContextReasonNotReached
	if !r.commandStarted.IsZero() {
		gapReason = telemetry.ContextReasonNotObserved
	}
	for _, kind := range []telemetry.ContextKind{"workspace", "java"} {
		if seen[kind] {
			continue
		}
		contexts = append(contexts, telemetry.Context{
			Kind:   kind,
			Status: telemetry.ContextAbsent,
			Reason: gapReason,
		})
	}
	if r.invocation.TelemetryEnabled {
		contexts = append(contexts, telemetry.Context{
			Kind:   "agent",
			Status: telemetry.ContextPresent,
			Data: struct {
				Detected bool `json:"detected"`
			}{Detected: agent.Detected()},
		})
	}
	return contexts
}

// normalizeObservedContext fills the status-specific reason an older command
// projection left empty. Present contexts never carry a reason.
func normalizeObservedContext(observed telemetry.Context) telemetry.Context {
	switch observed.Status {
	case telemetry.ContextAbsent:
		if observed.Reason == "" {
			observed.Reason = telemetry.ContextReasonNoResult
		}
	case telemetry.ContextFailed:
		if observed.Reason == "" {
			observed.Reason = telemetry.ContextReasonObservationFailed
		}
	}
	return observed
}

func (r *Runner) telemetryLocalLog() bool {
	return r.invocation.Logger != nil && r.invocation.Logger.FilePath() != ""
}

func (r *Runner) logTelemetrySubmission(kind string, submission telemetry.Submission, err error) {
	logger := r.invocation.Logger
	if logger == nil {
		return
	}
	switch {
	case err != nil:
		logger.Debug("telemetry " + kind + " enqueue failed")
	case submission.Queued:
		logger.Debug("telemetry " + kind + " queued")
	case submission.Reason == telemetry.SkipDisabled,
		submission.Reason == telemetry.SkipEnvironment:
		logger.Debug("telemetry "+kind+" skipped", "reason", string(submission.Reason))
	default:
		logger.Debug("telemetry "+kind+" not queued", "reason", string(submission.Reason))
	}
}

func (r *Runner) logTelemetryFlush(result telemetry.FlushResult, err error) {
	logger := r.invocation.Logger
	if logger == nil {
		return
	}
	if err != nil {
		logger.Debug("telemetry flush failed")
		return
	}
	if result.Skipped {
		logger.Debug("telemetry flush skipped", "reason", string(result.Reason))
		return
	}
	logger.Debug(
		"telemetry flush completed",
		"attempted", result.Attempted,
		"accepted", result.Accepted,
		"rejected", result.Rejected,
		"remaining", result.Remaining,
	)
}
