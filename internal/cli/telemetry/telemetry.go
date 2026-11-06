// Package telemetry adapts the local telemetry client to the `lucy telemetry`
// and `lucy agent-report` commands.
package telemetry

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/internal/cli"
	telem "github.com/mclucy/lucy/internal/telemetry"
	"github.com/spf13/cobra"
)

var (
	statsJSON  cli.JSONResult[*telem.Stats]
	actionJSON cli.JSONResult[*action]
)

// State is the telemetry handle the root wiring publishes to this command
// tree. The wiring fills it before commands execute, so handlers read the
// client at action time rather than at construction.
type State struct {
	// Client is the run's telemetry client. It stays nil until the root
	// wiring initializes telemetry, and when construction failed.
	Client *telem.Client
	// Err reports why Client is unavailable.
	Err error
	// Local reports a local development build, which disables collection even
	// when the saved preference is on.
	Local bool
}

// client returns the run's telemetry client or the reason it is unavailable.
func (s *State) client() (*telem.Client, error) {
	if s.Err != nil {
		return nil, fmt.Errorf("telemetry state: %w", s.Err)
	}
	if s.Client == nil {
		return nil, errors.New("telemetry state is unavailable")
	}
	return s.Client, nil
}

// NewCommand builds the telemetry command that controls collection and shows
// local statistics.
func NewCommand(
	contracts *cli.JSONContracts,
	inv *cli.Invocation,
	state *State,
) *cobra.Command {
	status := &cobra.Command{
		Use: "telemetry", Short: "Control telemetry and inspect local statistics", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return actionStats(cmd, inv, state) },
	}
	stats := &cobra.Command{
		Use: "stats", Short: "Show local telemetry upload statistics", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return actionStats(cmd, inv, state) },
	}
	statsJSON.Bind(contracts, status)
	statsJSON.Bind(contracts, stats)
	status.AddCommand(stats)
	for _, setting := range []struct {
		name    string
		enabled bool
	}{{"on", true}, {"off", false}} {
		command := &cobra.Command{
			Use: setting.name, Short: "Turn telemetry " + setting.name, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				client, err := state.client()
				if err != nil {
					return err
				}
				if err := client.SetEnabled(cmd.Context(), setting.enabled); err != nil {
					return err
				}
				message := "Telemetry is enabled."
				actionState := setting.name
				if !setting.enabled {
					message = "Telemetry is off; pending local records were deleted. Existing remote data was not changed."
				} else if enabled, err := client.Enabled(cmd.Context()); err != nil {
					return err
				} else if !enabled {
					actionState = "off"
					if state.Local {
						message = "The saved preference is on, but telemetry is disabled in local development builds."
					} else {
						message = "The saved preference is on, but telemetry remains off due to the environment."
					}
				}
				return writeAction(cmd, inv, &action{
					Action:  setting.name,
					State:   actionState,
					Message: message,
				})
			},
		}
		actionJSON.Bind(contracts, command)
		status.AddCommand(command)
	}
	reset := &cobra.Command{
		Use: "reset-id", Short: "Reset the client ID and local statistics", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := state.client()
			if err != nil {
				return err
			}
			if _, err := client.ResetIdentity(cmd.Context()); err != nil {
				return err
			}
			return writeAction(cmd, inv, &action{
				Action:  "reset-id",
				State:   "reset",
				Message: "The client ID and local statistics were reset. Existing remote data was not changed.",
			})
		},
	}
	actionJSON.Bind(contracts, reset)
	status.AddCommand(reset)
	return status
}

// IsCommand reports whether command belongs to the telemetry command tree.
func IsCommand(command *cobra.Command) bool {
	for current := command; current != nil; current = current.Parent() {
		if current.Name() == "telemetry" || current.Name() == "agent-report" {
			return true
		}
	}
	return false
}

// action is the JSON result of a telemetry control command.
type action struct {
	Action   string `json:"action"`
	State    string `json:"state"`
	RecordID string `json:"record_id,omitempty"`
	Message  string `json:"message,omitempty"`
}

func writeAction(cmd *cobra.Command, inv *cli.Invocation, value *action) error {
	output := cli.NewQueryOutput(cmd, inv)
	if output.JSON() {
		return actionJSON.Write(output, value)
	}
	_, err := fmt.Fprintln(output.Human(), value.Message)
	return err
}
