package cli

import (
	"fmt"

	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/internal/telemetry"
	"github.com/mclucy/lucy/log"
	"github.com/spf13/cobra"
)

const (
	flagTelemetryTokenStdinName = "token-stdin"
	flagTelemetryModelName      = "model"
	flagTelemetryHarnessName    = "harness"
)

const telemetrySchema = 1

type telemetryStateDocument struct {
	Schema  int    `json:"schema"`
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
}

type telemetryReportDocument struct {
	Schema   int    `json:"schema"`
	Kind     string `json:"kind"`
	Uploaded bool   `json:"uploaded"`
}

// NewTelemetryCommand returns the telemetry management command family. The
// upload entry is internal and never appears in help.
func NewTelemetryCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "telemetry",
		Short: help.TelemetryShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(
		newTelemetryOnCommand(),
		newTelemetryOffCommand(),
		newTelemetryReportCommand(),
		newTelemetryUploadCommand(),
	)
	return command
}

func newTelemetryOnCommand() *cobra.Command {
	var tokenStdin bool
	command := &cobra.Command{
		Use:   "on",
		Short: help.TelemetryOnShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			output := NewOperationOutput(cmd)
			result, err := telemetry.Enable(tokenStdin, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if output.JSON() {
				return output.WriteJSON(telemetryStateDocument{
					Schema:  telemetrySchema,
					Kind:    "telemetry",
					Enabled: result.Enabled,
				})
			}
			fmt.Fprintln(output.Human(), "Telemetry enabled.")
			return nil
		},
	}
	command.Flags().BoolVar(
		&tokenStdin,
		flagTelemetryTokenStdinName,
		false,
		"Read a new upload token from standard input",
	)
	return command
}

func newTelemetryOffCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: help.TelemetryOffShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			output := NewOperationOutput(cmd)
			if err := telemetry.Disable(); err != nil {
				return err
			}
			if output.JSON() {
				return output.WriteJSON(telemetryStateDocument{
					Schema:  telemetrySchema,
					Kind:    "telemetry",
					Enabled: false,
				})
			}
			fmt.Fprintln(output.Human(), "Telemetry disabled.")
			return nil
		},
	}
}

func newTelemetryReportCommand() *cobra.Command {
	var model, harness string
	command := &cobra.Command{
		Use:    "agent-report",
		Short:  help.TelemetryReportShort,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			output := NewOperationOutput(cmd)
			if err := telemetry.ReportReady(); err != nil {
				return err
			}
			body, err := telemetry.ReadReportBody(cmd.InOrStdin())
			if err != nil {
				return err
			}
			if err := telemetry.SendReport(body, model, harness, output.JSON()); err != nil {
				return err
			}
			if output.JSON() {
				return output.WriteJSON(telemetryReportDocument{
					Schema:   telemetrySchema,
					Kind:     "telemetry-report",
					Uploaded: true,
				})
			}
			fmt.Fprintln(output.Human(), "Report uploaded.")
			return nil
		},
	}
	command.Flags().StringVar(&model, flagTelemetryModelName, "", "Model that produced the report")
	command.Flags().StringVar(&harness, flagTelemetryHarnessName, "", "Agent harness that produced the report")
	return command
}

func newTelemetryUploadCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "upload <handoff>",
		Short:  help.TelemetryUploadShort,
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := telemetry.UploadHandoff(args[0]); err != nil {
				log.Debug("telemetry upload finished", "uploaded", false, "error", err)
				return SilentExit(1)
			}
			log.Debug("telemetry upload finished", "uploaded", true)
			return nil
		},
	}
}
