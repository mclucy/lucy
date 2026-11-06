package telemetry

import (
	"fmt"
	"time"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/spf13/cobra"
)

func actionStats(cmd *cobra.Command, inv *cli.Invocation, state *State) error {
	client, err := state.client()
	if err != nil {
		return err
	}
	stats, err := client.Stats(cmd.Context())
	if err != nil {
		return err
	}
	output := cli.NewQueryOutput(cmd, inv)
	if output.JSON() {
		return statsJSON.Write(output, &stats)
	}
	telemetryState := "on"
	if !stats.Enabled {
		telemetryState = "off"
	}
	if stats.Reason != "" {
		telemetryState += " (" + string(stats.Reason) + ")"
	}
	destination := stats.Endpoint
	if destination == "" {
		destination = "not configured"
	}
	_, err = fmt.Fprintf(
		output.Human(),
		"Telemetry: %s\nClient ID: %s\nStatistics since: %s\n"+
			"Destination: %s\nAccepted records: %d (events: %d, reports: %d)\n"+
			"Accepted event payload: %d bytes\nAccepted report content: %d bytes\n"+
			"Upload request bodies: %d bytes in %d attempts (%d uncertain)\n"+
			"Pending records: %d (events: %d, reports: %d; %d bytes)\n"+
			"Dropped records: %d (%d bytes)\nRejected records: %d (%d bytes)\n\n"+
			"Request bytes include retries but exclude HTTP and TLS overhead. "+
			"Accepted content sizes do not describe current remote storage.\n",
		telemetryState, stats.InstallationID, stats.PeriodStart.Format(time.RFC3339),
		destination, stats.Accepted.Records, stats.Accepted.Events, stats.Accepted.Reports,
		stats.Accepted.EventPayloadBytes, stats.Accepted.ReportBodyBytes,
		stats.Transport.RequestBodyBytes, stats.Transport.Requests, stats.Transport.Uncertain,
		stats.Queue.Records, stats.Queue.Events, stats.Queue.Reports, stats.Queue.PayloadBytes,
		stats.Dropped.Records, stats.Dropped.Bytes, stats.Rejected.Records,
		stats.Rejected.PayloadBytes,
	)
	return err
}
