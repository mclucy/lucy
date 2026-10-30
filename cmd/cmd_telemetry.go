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
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/telemetry"
	"github.com/spf13/cobra"
)

var (
	telemetryStatsJSON  cli.JSONResult[*telemetry.Stats]
	telemetryActionJSON cli.JSONResult[*telemetryAction]
)

type telemetryAction struct {
	Action   string `json:"action"`
	State    string `json:"state"`
	RecordID string `json:"record_id,omitempty"`
	Message  string `json:"message,omitempty"`
}

func (r *Runner) registerTelemetryCommands() {
	status := &cobra.Command{
		Use: "telemetry", Short: "Control telemetry and inspect local statistics", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return r.telemetryStats(cmd) },
	}
	stats := &cobra.Command{
		Use: "stats", Short: "Show local telemetry upload statistics", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return r.telemetryStats(cmd) },
	}
	telemetryStatsJSON.Bind(r.contracts, status)
	telemetryStatsJSON.Bind(r.contracts, stats)
	status.AddCommand(stats)
	for _, setting := range []struct {
		name    string
		enabled bool
	}{{"on", true}, {"off", false}} {
		command := &cobra.Command{
			Use: setting.name, Short: "Turn telemetry " + setting.name, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if err := r.requireTelemetry(); err != nil {
					return err
				}
				if err := r.telemetry.SetEnabled(cmd.Context(), setting.enabled); err != nil {
					return err
				}
				message := "Telemetry is enabled."
				state := setting.name
				if !setting.enabled {
					message = "Telemetry is off; pending local records were deleted. Existing remote data was not changed."
				} else if enabled, err := r.telemetry.Enabled(cmd.Context()); err != nil {
					return err
				} else if !enabled {
					state = "off"
					message = "The saved preference is on, but telemetry remains off due to the environment."
				}
				return r.writeTelemetryAction(cmd, &telemetryAction{Action: setting.name, State: state, Message: message})
			},
		}
		telemetryActionJSON.Bind(r.contracts, command)
		status.AddCommand(command)
	}
	reset := &cobra.Command{
		Use: "reset-id", Short: "Reset the client ID and local statistics", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := r.requireTelemetry(); err != nil {
				return err
			}
			if _, err := r.telemetry.ResetIdentity(cmd.Context()); err != nil {
				return err
			}
			return r.writeTelemetryAction(cmd, &telemetryAction{Action: "reset-id", State: "reset", Message: "The client ID and local statistics were reset. Existing remote data was not changed."})
		},
	}
	telemetryActionJSON.Bind(r.contracts, reset)
	status.AddCommand(reset)
	agentReport := &cobra.Command{
		Use: "agent-report [CONTENT]", Short: "Submit an agent report", Hidden: true, Args: cobra.ExactArgs(1),
		Long: "Submit one privacy-filtered report about a Lucy failure or friction in this session.\n" +
			"The argument is the report body. Write one to three sentences: the command, the error code or\n" +
			"observed behavior, and what you expected instead. Add --agent and --model when you know them.\n" +
			"Leave out secrets, absolute paths, logs, transcripts, and user data; Lucy also redacts common\n" +
			"patterns before upload. Skip failures caused by your own wrong directory, wrong flag, or missing\n" +
			"prerequisite; report unexpected behavior even when a workaround works. Report each distinct\n" +
			"problem once. The result is queued, accepted, or rejected, and a rejected report needs no repair.",
		RunE: func(cmd *cobra.Command, args []string) error { return r.submitAgentReport(cmd, args[0]) },
	}
	agentReport.Flags().String("agent", "", "Agent name")
	agentReport.Flags().String("model", "", "Model name")
	telemetryActionJSON.Bind(r.contracts, agentReport)
	r.root.AddCommand(status, agentReport)
}

func (r *Runner) requireTelemetry() error {
	if r.telemetryErr != nil {
		return fmt.Errorf("telemetry state: %w", r.telemetryErr)
	}
	if r.telemetry == nil {
		return errors.New("telemetry state is unavailable")
	}
	return nil
}

func (r *Runner) telemetryStats(cmd *cobra.Command) error {
	if err := r.requireTelemetry(); err != nil {
		return err
	}
	stats, err := r.telemetry.Stats(cmd.Context())
	if err != nil {
		return err
	}
	output := cli.NewQueryOutput(cmd, r.invocation)
	if output.JSON() {
		return telemetryStatsJSON.Write(output, &stats)
	}
	state := "on"
	if !stats.Enabled {
		state = "off"
	}
	if stats.Reason != "" {
		state += " (" + string(stats.Reason) + ")"
	}
	destination := stats.Endpoint
	if destination == "" {
		destination = "not configured"
	}
	_, err = fmt.Fprintf(output.Human(),
		"Telemetry: %s\nClient ID: %s\nStatistics since: %s\n"+
			"Destination: %s\nAccepted records: %d (events: %d, reports: %d)\n"+
			"Accepted event payload: %d bytes\nAccepted report content: %d bytes\n"+
			"Upload request bodies: %d bytes in %d attempts (%d uncertain)\n"+
			"Pending records: %d (events: %d, reports: %d; %d bytes)\n"+
			"Dropped records: %d (%d bytes)\nRejected records: %d (%d bytes)\n\n"+
			"Request bytes include retries but exclude HTTP and TLS overhead. "+
			"Accepted content sizes do not describe current remote storage.\n",
		state, stats.InstallationID, stats.PeriodStart.Format(time.RFC3339),
		destination, stats.Accepted.Records, stats.Accepted.Events, stats.Accepted.Reports,
		stats.Accepted.EventPayloadBytes, stats.Accepted.ReportBodyBytes,
		stats.Transport.RequestBodyBytes, stats.Transport.Requests, stats.Transport.Uncertain,
		stats.Queue.Records, stats.Queue.Events, stats.Queue.Reports, stats.Queue.PayloadBytes,
		stats.Dropped.Records, stats.Dropped.Bytes, stats.Rejected.Records,
		stats.Rejected.PayloadBytes,
	)
	return err
}

func (r *Runner) writeTelemetryAction(cmd *cobra.Command, action *telemetryAction) error {
	output := cli.NewQueryOutput(cmd, r.invocation)
	if output.JSON() {
		return telemetryActionJSON.Write(output, action)
	}
	_, err := fmt.Fprintln(output.Human(), action.Message)
	return err
}

func (r *Runner) submitAgentReport(cmd *cobra.Command, body string) error {
	if err := r.requireTelemetry(); err != nil {
		return err
	}
	agentName, _ := cmd.Flags().GetString("agent")
	model, _ := cmd.Flags().GetString("model")
	result, err := r.telemetry.SubmitReport(cmd.Context(), telemetry.Report{Kind: telemetry.ReportKind("agent_feedback"), Agent: agentName, Model: model, Body: body})
	if err != nil {
		return err
	}
	if !result.Queued {
		message := "Report was not queued: " + string(result.Reason) + "."
		if result.Reason == telemetry.SkipReason("disabled") || result.Reason == telemetry.SkipReason("environment_disabled") {
			message = "Telemetry is off; the report was not submitted. This is not an error to repair. Never enable telemetry without explicit user instruction."
		}
		return r.writeTelemetryAction(cmd, &telemetryAction{Action: "agent-report", State: "rejected", Message: message})
	}
	flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	flush, flushErr := r.telemetry.Flush(flushCtx)
	state := "queued"
	message := "Report queued locally; it has not been accepted remotely."
	if flushErr == nil && flush.WasAccepted(result.RecordID) {
		state = "accepted"
		message = "Report accepted remotely; processing may still be pending."
	} else if flushErr == nil && flush.WasRejected(result.RecordID) {
		state = "rejected"
		message = "The service rejected the report; it is no longer queued."
	}
	if result.Truncated {
		message += " The report was truncated to the allowed size."
	}
	return r.writeTelemetryAction(cmd, &telemetryAction{Action: "agent-report", State: state, RecordID: result.RecordID, Message: message})
}

func (r *Runner) startTelemetry() time.Time {
	started := time.Now()
	r.invocation.TelemetryEnabled = false
	r.invocation.TelemetryContexts = nil
	version, commit := buildVersion()
	r.telemetryUnidentified = version == ""
	r.telemetry, r.telemetryErr = telemetry.New(telemetry.Config{
		Endpoint: os.Getenv("LUCY_TELEMETRY_ENDPOINT"), Channel: telemetryChannel(version),
		LucyVersion: version, LucyCommit: commit, OS: runtime.GOOS, Arch: runtime.GOARCH, HTTPClient: r.httpClient,
	})
	return started
}

// telemetryChannel derives the release channel from the build version. Only
// alpha and beta channels collect by default, so a stable release must not
// inherit the alpha default. An unidentified build is a development build; it
// records nothing, but it still retries records queued by identified builds.
func telemetryChannel(version string) string {
	lower := strings.ToLower(version)
	switch {
	case strings.Contains(lower, "beta"):
		return "beta"
	case lower == "", strings.Contains(lower, "alpha"), lower == "dev", lower == "devel", strings.HasPrefix(lower, "devel-"):
		return "alpha"
	default:
		return "stable"
	}
}

func isTelemetryCommand(command *cobra.Command) bool {
	for current := command; current != nil; current = current.Parent() {
		if current.Name() == "telemetry" || current.Name() == "agent-report" {
			return true
		}
	}
	return false
}

func (r *Runner) finishTelemetry(started time.Time, commandErr error) {
	if r.telemetry == nil {
		return
	}
	defer r.telemetry.Close()
	if r.telemetryUnidentified {
		return
	}
	command := r.invocationCommand()
	if isTelemetryCommand(command) || command.Name() == "__complete" || command.Name() == "__completeNoDesc" {
		return
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = r.telemetry.RecordEvent(ctx, telemetry.Event{
		InvocationID: id, Command: strings.TrimPrefix(command.CommandPath(), "lucy "),
		Duration: time.Since(started), Outcome: outcome, ErrorCode: code,
		OccurredAt: started, Contexts: r.invocation.TelemetryContexts,
	})
	if reportKind != "" {
		body := "Command: " + command.CommandPath() + "\nError category: " + code + "\n"
		if stages := r.invocation.Logger.Stages(); len(stages) > 0 {
			body += "Stages:\n" + strings.Join(stages, "\n") + "\n"
		}
		if panicFailure, ok := errors.AsType[*panicError](commandErr); ok && panicFailure != nil {
			if functions := telemetryPanicFunctions(panicFailure.stack); len(functions) > 0 {
				body += "Lucy stack functions:\n" + strings.Join(functions, "\n") + "\n"
			}
		}
		_, _ = r.telemetry.SubmitReport(ctx, telemetry.Report{Kind: reportKind, InvocationID: id, Body: body})
	}
	if offline := command.Flags().Lookup("offline"); offline != nil && offline.Value.String() == "true" {
		return
	}
	flush, flushErr := r.telemetry.Flush(ctx)
	if flushErr == nil && flush.Attempted > 0 && !r.JSONSelected() && r.stderr != nil {
		fmt.Fprintln(r.stderr, "Telemetry upload attempted; run `lucy telemetry stats` for local totals.")
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
