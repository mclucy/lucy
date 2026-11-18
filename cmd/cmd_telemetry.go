package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/mclucy/lucy/internal/cli"
	telemetrycli "github.com/mclucy/lucy/internal/cli/telemetry"
	"github.com/mclucy/lucy/internal/telemetry"
	"github.com/mclucy/lucy/terminal/report"
	"github.com/spf13/cobra"
)

// registerTelemetryCommands attaches the telemetry control and agent report
// commands. Telemetry initialization stays with the runner: the commands read
// the state this method publishes through a live handle.
func (r *Runner) registerTelemetryCommands() {
	r.root.AddCommand(
		telemetrycli.NewCommand(r.contracts, r.invocation, &r.telemetryState),
		telemetrycli.NewAgentReportCommand(r.contracts, r.invocation, &r.telemetryState),
	)
}

func (r *Runner) startTelemetry() time.Time {
	started := time.Now()
	r.invocation.TelemetryEnabled = false
	r.invocation.TelemetryObservation = nil
	r.invocation.TelemetryContexts = nil
	version, commit := buildVersion()
	local := !isTelemetryRelease()
	client, err := telemetry.New(telemetry.Config{
		Disabled:    local,
		Endpoint:    os.Getenv("LUCY_TELEMETRY_ENDPOINT"),
		Channel:     telemetryChannel(version),
		LucyVersion: version,
		LucyCommit:  commit,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		HTTPClient:  r.httpClient,
	})
	r.telemetryState = telemetrycli.State{Client: client, Err: err, Local: local}
	return started
}

// telemetryChannel derives the release channel from the build version. Only
// alpha and beta channels collect by default, so a stable release must not
// inherit the alpha default.
func telemetryChannel(version string) string {
	lower := strings.ToLower(version)
	switch {
	case strings.Contains(lower, "beta"):
		return "beta"
	case strings.Contains(lower, "alpha"):
		return "alpha"
	default:
		return "stable"
	}
}

func (r *Runner) finishTelemetry(started time.Time, commandErr error) {
	client := r.telemetryState.Client
	if client == nil {
		return
	}
	defer client.Close()
	command := r.invocationCommand()
	policy := invocationTelemetryPolicy(command, commandErr)
	if !policy.collect {
		return
	}
	ended := time.Now()
	if ended.Before(started) {
		ended = started
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r.recordInvocation(ctx, command, started, ended, commandErr)
	if !policy.flush {
		r.invocation.Logger.Debug("telemetry flush skipped", "reason", "offline")
		return
	}
	flush, flushErr := client.Flush(ctx)
	r.logTelemetryFlush(flush, flushErr)
	if flushErr == nil && flush.Attempted > 0 && !r.JSONSelected() && r.stderr != nil {
		fmt.Fprintln(r.stderr, "Telemetry upload attempted; run `lucy telemetry stats` for local totals.")
	}
}

type telemetryInvocationPolicy struct {
	collect bool
	flush   bool
}

// invocationTelemetryPolicy governs automatic telemetry, not explicit
// telemetry commands. Failed guidance invocations remain observable, but a
// successful schema inspection is guidance and does not queue or flush.
func invocationTelemetryPolicy(command *cobra.Command, commandErr error) telemetryInvocationPolicy {
	if cli.TelemetryExempt(command) {
		return telemetryInvocationPolicy{}
	}
	for current := command; current.Parent() != nil; current = current.Parent() {
		if current.Parent() == current.Root() {
			switch current.Name() {
			case "completion", "__complete", "__completeNoDesc", "help", "man":
				return telemetryInvocationPolicy{}
			case "schema":
				if commandErr == nil {
					return telemetryInvocationPolicy{}
				}
			}
		}
	}
	if commandErr == nil {
		if cli.IsHelpOnly(command) || !command.Runnable() {
			return telemetryInvocationPolicy{}
		}
		for current := command; current != nil; current = current.Parent() {
			for _, name := range []string{"help", "version"} {
				if flag := current.Flags().Lookup(name); flag != nil && flag.Value.String() == "true" {
					return telemetryInvocationPolicy{}
				}
			}
		}
	}
	policy := telemetryInvocationPolicy{collect: true, flush: true}
	if telemetryBoolFlag(command, "offline") {
		policy.flush = false
	}
	return policy
}

// recordInvocation records the terminal event and, for failures, a diagnostic
// report. An unidentified build skips this call so harness traffic cannot pass
// as user activity, but it still flushes records queued by identified builds.
func (r *Runner) recordInvocation(ctx context.Context, command *cobra.Command, started, ended time.Time, commandErr error) {
	client := r.telemetryState.Client
	usageError := r.telemetryUsageError(commandErr)
	outcome := telemetry.Outcome("success")
	code := ""
	reportKind := telemetry.ReportKind("")
	if commandErr != nil {
		outcome = telemetry.Outcome("failure")
		code = cli.CodeCommandFailed
		_, panicked := errors.AsType[*panicError](commandErr)
		exit, hasExit := errors.AsType[*cli.ExitError](commandErr)
		canceled := errors.Is(commandErr, context.Canceled) ||
			errors.Is(commandErr, context.DeadlineExceeded)
		switch {
		case canceled:
			outcome, code = telemetry.Outcome("canceled"), cli.CodeOperationCancelled
		case usageError:
			code = cli.CodeInvalidInvocation
		case panicked:
			reportKind = telemetry.ReportKind("panic")
		case hasExit && exit != nil:
			if !exit.Silent {
				reportKind = telemetry.ReportKind("failure")
			}
		default:
			reportKind = telemetry.ReportKind("failure")
		}
		if detail, ok := errors.AsType[*report.Detail](commandErr); ok && detail != nil && detail.Code != "" {
			code = detail.Code
		} else if code == cli.CodeCommandFailed {
			if mapped := telemetryFailureCode(commandErr); mapped != "" {
				code = mapped
			}
		}
	}

	r.completeCommandObservation(ended, commandErr)
	localLog := r.telemetryLocalLog()
	failureKind := r.telemetryFailureKind(commandErr, usageError)
	cancelReason := r.telemetryCancelReason(
		commandErr,
		r.invocation.TelemetryObservation.PromptAborted(),
	)
	id := telemetry.NewUUID()
	if localLog {
		r.invocation.Logger.Debug("telemetry invocation", "invocation_id", id)
	}
	event := telemetry.Event{
		InvocationID:     id,
		Command:          strings.TrimPrefix(command.CommandPath(), "lucy "),
		Duration:         ended.Sub(started),
		DurationMeasured: true,
		Outcome:          outcome,
		ErrorCode:        code,
		OccurredAt:       started,
		Contexts: r.buildTelemetryContexts(
			command,
			usageError,
			failureKind,
			cancelReason,
			started,
			ended,
			localLog,
		),
	}
	submission, enqueueErr := client.RecordEvent(ctx, event)
	r.logTelemetrySubmission("event", submission, enqueueErr)
	if reportKind != "" {
		body := cli.ReportBody{
			Command: command.CommandPath(),
			Code:    code,
			Stages:  cli.StageCounts(r.invocation.Logger.Stages()),
			Facts:   cli.FailureFacts(commandErr),
		}
		if commandErr != nil {
			body.Message = commandErr.Error()
		}
		if panicFailure, ok := errors.AsType[*panicError](commandErr); ok && panicFailure != nil {
			body.Stack = telemetryPanicFunctions(panicFailure.stack)
		}
		reportSubmission, reportErr := client.SubmitReport(ctx, telemetry.Report{
			Kind:         reportKind,
			InvocationID: id,
			Body:         body.Encode(),
		})
		r.logTelemetrySubmission("report", reportSubmission, reportErr)
	}
}

func telemetryPanicFunctions(stack []byte) []string {
	var functions []string
	for _, line := range strings.Split(string(stack), "\n") {
		if !strings.HasPrefix(line, "github.com/mclucy/lucy/") {
			continue
		}
		name, _, found := strings.CutLast(line, "(")
		if !found || len(name) > 160 {
			continue
		}
		if strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/-()*") != "" {
			continue
		}
		functions = append(functions, name)
		if len(functions) == 32 {
			break
		}
	}
	return functions
}
