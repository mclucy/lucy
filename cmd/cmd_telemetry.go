package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/mclucy/lucy/internal/agent"
	"github.com/mclucy/lucy/internal/cli"
	telemetrycli "github.com/mclucy/lucy/internal/cli/telemetry"
	"github.com/mclucy/lucy/internal/telemetry"
	"github.com/mclucy/lucy/report"
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
	if telemetrycli.IsCommand(command) || command.Name() == "__complete" || command.Name() == "__completeNoDesc" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r.recordInvocation(ctx, command, started, commandErr)
	if noOpExit(command, commandErr) {
		return
	}
	if offline := command.Flags().Lookup("offline"); offline != nil && offline.Value.String() == "true" {
		return
	}
	flush, flushErr := client.Flush(ctx)
	if flushErr == nil && flush.Attempted > 0 && !r.JSONSelected() && r.stderr != nil {
		fmt.Fprintln(r.stderr, "Telemetry upload attempted; run `lucy telemetry stats` for local totals.")
	}
}

// noOpExit reports an invocation that displayed guidance instead of running a
// command: help for the addressed command, the help command, a version
// request, or a command group without a subcommand. The event stays queued, so
// a machine that only reads help sends nothing.
func noOpExit(command *cobra.Command, commandErr error) bool {
	if commandErr != nil {
		return false
	}
	if cli.IsHelpOnly(command) || command.Name() == "help" {
		return true
	}
	for current := command; current != nil; current = current.Parent() {
		for _, name := range []string{"help", "version"} {
			if flag := current.Flags().Lookup(name); flag != nil && flag.Value.String() == "true" {
				return true
			}
		}
	}
	return !command.Runnable()
}

// recordInvocation records the terminal event and, for failures, a diagnostic
// report. An unidentified build skips this call so harness traffic cannot pass
// as user activity, but it still flushes records queued by identified builds.
func (r *Runner) recordInvocation(ctx context.Context, command *cobra.Command, started time.Time, commandErr error) {
	client := r.telemetryState.Client
	outcome := telemetry.Outcome("success")
	code := ""
	reportKind := telemetry.ReportKind("")
	if commandErr != nil {
		outcome = telemetry.Outcome("failure")
		code = cli.CodeCommandFailed
		_, panicked := errors.AsType[*panicError](commandErr)
		exit, hasExit := errors.AsType[*cli.ExitError](commandErr)
		switch {
		case errors.Is(commandErr, context.Canceled), errors.Is(commandErr, context.DeadlineExceeded):
			outcome, code = telemetry.Outcome("canceled"), cli.CodeOperationCancelled
		case cli.IsUsageError(commandErr):
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
		}
	}
	if r.invocation.TelemetryEnabled {
		r.invocation.TelemetryContexts = append(r.invocation.TelemetryContexts, telemetry.Context{
			Kind: "agent", Status: telemetry.ContextPresent,
			Data: struct {
				Detected bool `json:"detected"`
			}{Detected: agent.Detected()},
		})
	}
	id := telemetry.NewUUID()
	_, _ = client.RecordEvent(ctx, telemetry.Event{
		InvocationID: id,
		Command:      strings.TrimPrefix(command.CommandPath(), "lucy "),
		Duration:     time.Since(started),
		Outcome:      outcome,
		ErrorCode:    code,
		OccurredAt:   started,
		Contexts:     r.invocation.TelemetryContexts,
	})
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
		_, _ = client.SubmitReport(ctx, telemetry.Report{Kind: reportKind, InvocationID: id, Body: body.Encode()})
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
