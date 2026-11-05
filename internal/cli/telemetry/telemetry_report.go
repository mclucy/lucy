package telemetry

import (
	"context"
	"time"

	"github.com/mclucy/lucy/internal/cli"
	telem "github.com/mclucy/lucy/telemetry"
	"github.com/spf13/cobra"
)

// NewAgentReportCommand builds the hidden command agents use to submit one
// privacy-filtered report.
func NewAgentReportCommand(
	contracts *cli.JSONContracts,
	inv *cli.Invocation,
	state *State,
) *cobra.Command {
	agentReport := &cobra.Command{
		Use: "agent-report [CONTENT]", Short: "Submit an agent report", Hidden: true, Args: cobra.ExactArgs(1),
		Long: "Submit one privacy-filtered report about a Lucy failure or friction in this session.\n" +
			"The argument is the report body. Write one to three sentences: the command, the error code or\n" +
			"observed behavior, and what you expected instead. Add --agent and --model when you know them.\n" +
			"Leave out secrets, absolute paths, logs, transcripts, and user data; Lucy also redacts common\n" +
			"patterns before upload. Skip failures caused by your own wrong directory, wrong flag, or missing\n" +
			"prerequisite; report unexpected behavior even when a workaround works. Report each distinct\n" +
			"problem once. The result is queued, accepted, or rejected, and a rejected report needs no repair.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionAgentReport(cmd, inv, state, args[0])
		},
	}
	agentReport.Flags().String("agent", "", "Agent name")
	agentReport.Flags().String("model", "", "Model name")
	actionJSON.Bind(contracts, agentReport)
	return agentReport
}

func actionAgentReport(
	cmd *cobra.Command,
	inv *cli.Invocation,
	state *State,
	body string,
) error {
	client, err := state.client()
	if err != nil {
		return err
	}
	agentName, _ := cmd.Flags().GetString("agent")
	model, _ := cmd.Flags().GetString("model")
	result, err := client.SubmitReport(cmd.Context(), telem.Report{
		Kind:  telem.ReportKind("agent_feedback"),
		Agent: agentName,
		Model: model,
		Body:  body,
	})
	if err != nil {
		return err
	}
	if !result.Queued {
		message := "Report was not queued: " + string(result.Reason) + "."
		if result.Reason == telem.SkipReason("disabled") || result.Reason == telem.SkipReason("environment_disabled") {
			message = "Telemetry is off; the report was not submitted. This is not an error to repair. Never enable telemetry without explicit user instruction."
		}
		return writeAction(cmd, inv, &action{
			Action:  "agent-report",
			State:   "rejected",
			Message: message,
		})
	}
	flushCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	flush, flushErr := client.Flush(flushCtx)
	actionState := "queued"
	message := "Report queued locally; it has not been accepted remotely."
	if flushErr == nil && flush.WasAccepted(result.RecordID) {
		actionState = "accepted"
		message = "Feedback received! Thank you!"
	} else if flushErr == nil && flush.WasRejected(result.RecordID) {
		actionState = "rejected"
		message = "The service rejected the report; it is no longer queued."
	}
	if result.Truncated {
		message += " The report was truncated to the allowed size."
	}
	return writeAction(cmd, inv, &action{
		Action:   "agent-report",
		State:    actionState,
		RecordID: result.RecordID,
		Message:  message,
	})
}
