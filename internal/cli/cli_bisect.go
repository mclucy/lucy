package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/internal/bisect"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/internal/depgraph"
	"github.com/spf13/cobra"
)

// bisectJSON is the shared machine-readable result of every bisect leaf: they
// all report progress through bisectOutput.
var bisectJSON JSONResult[bisectOutput]

func NewBisectCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	bisectStartCmd := &cobra.Command{
		Use:   "start",
		Short: help.BisectStartShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectStart(cmd, inv, deps, args)
		},
	}
	bisectGoodCmd := &cobra.Command{
		Use:   "good",
		Short: help.BisectGoodShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectGood(cmd, inv, deps, args)
		},
	}
	bisectBadCmd := &cobra.Command{
		Use:   "bad",
		Short: help.BisectBadShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectBad(cmd, inv, deps, args)
		},
	}
	bisectStatusCmd := &cobra.Command{
		Use:   "status",
		Short: help.BisectStatusShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectStatus(cmd, inv, deps, args)
		},
	}
	bisectResetCmd := &cobra.Command{
		Use:   "reset",
		Short: help.BisectResetShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionBisectReset(cmd, inv, deps, args)
		},
	}
	bisectCmd := &cobra.Command{
		Use:   "bisect",
		Short: help.BisectShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	MarkHelpOnly(bisectCmd)
	bisectJSON.Bind(contracts, bisectStartCmd)
	bisectJSON.Bind(contracts, bisectGoodCmd)
	bisectJSON.Bind(contracts, bisectBadCmd)
	bisectJSON.Bind(contracts, bisectStatusCmd)
	bisectJSON.Bind(contracts, bisectResetCmd)
	bisectCmd.AddCommand(
		bisectStartCmd,
		bisectGoodCmd,
		bisectBadCmd,
		bisectStatusCmd,
		bisectResetCmd,
	)
	return bisectCmd
}

type bisectOutput struct {
	Message  string       `json:"message"`
	Phase    bisect.Phase `json:"phase,omitempty"`
	Pending  bisect.Step  `json:"pending,omitempty"`
	Complete bool         `json:"complete"`
	Found    *bisect.Mod  `json:"found,omitempty"`
	State    *bisect.View `json:"state,omitempty"`
	Enabled  int          `json:"enabled"`
	Disabled int          `json:"disabled"`
	Restored int          `json:"restored"`
	Warnings []string     `json:"warnings,omitempty"`
}

// reportBisectFindings renders the live observation candidate selection
// consumed and returns its warnings for the result document.
func reportBisectFindings(
	cmd *cobra.Command,
	inv *Invocation,
	findings depgraph.Findings,
) []string {
	if findings.Observation == nil {
		return nil
	}
	output := NewOperationOutput(cmd, inv)
	reportObservationFindings(findings.Observation, output)
	return output.Warnings()
}

func bisectOutputFrom(result bisect.Result) bisectOutput {
	return bisectOutput{
		Phase:    result.Phase,
		Pending:  result.Pending,
		Complete: result.Complete,
		Found:    result.Found,
		State:    result.State,
		Enabled:  result.Enabled,
		Disabled: result.Disabled,
		Restored: result.Restored,
		Warnings: result.Warnings,
	}
}

func outputBisect(
	cmd *cobra.Command,
	inv *Invocation,
	view bisectOutput,
) error {
	output := NewOperationOutput(cmd, inv)
	if cmd.Name() == "status" && cmd.Parent() != nil && cmd.Parent().Name() == "bisect" {
		output = NewQueryOutput(cmd, inv)
	}
	if output.JSON() {
		return bisectJSON.Write(output, view)
	}
	if _, err := fmt.Fprintln(output.Human(), formatBisectOutput(view)); err != nil {
		return err
	}
	for _, warning := range view.Warnings {
		output.Warn(errors.New(warning))
	}
	return nil
}

func formatBisectOutput(output bisectOutput) string {
	var builder strings.Builder
	builder.WriteString("Bisect: ")
	builder.WriteString(output.Message)
	builder.WriteByte('\n')
	if output.State != nil {
		builder.WriteString(
			fmt.Sprintf(
				"Range: [%d, %d]\n",
				output.State.Left,
				output.State.Right,
			),
		)
		builder.WriteString(
			fmt.Sprintf(
				"Remaining: %d of %d\n",
				output.State.Remaining,
				output.State.Total,
			),
		)
		if output.State.Candidate != nil {
			builder.WriteString(
				fmt.Sprintf(
					"Candidate: %s (midpoint %d)\n",
					output.State.Candidate.Label(),
					output.State.Midpoint,
				),
			)
		}
	}
	if output.Found != nil {
		builder.WriteString("Bad package: ")
		builder.WriteString(output.Found.Label())
		builder.WriteByte('\n')
	}
	if output.Enabled > 0 || output.Disabled > 0 || output.Restored > 0 {
		builder.WriteString(
			fmt.Sprintf(
				"Changed: enabled %d, disabled %d, restored %d\n",
				output.Enabled,
				output.Disabled,
				output.Restored,
			),
		)
	}
	if output.Pending == bisect.StepReset {
		builder.WriteString("Pending: rerun `lucy bisect reset` to complete the interrupted reset.\n")
	} else if output.Pending != "" {
		builder.WriteString(
			fmt.Sprintf(
				"Pending: rerun `lucy bisect %s` to complete the interrupted transition or `lucy bisect reset` to abandon the session.\n",
				output.Pending,
			),
		)
	}
	if !output.Complete && output.Pending == "" && output.State != nil && output.State.Candidate != nil {
		builder.WriteString("Start the server, test it, then run `lucy bisect good` or `lucy bisect bad`.")
	}
	return strings.TrimRight(builder.String(), "\n")
}

// bisectTransitionMessage names the outcome of the transition a leaf
// completed.
func bisectTransitionMessage(step bisect.Step, result bisect.Result) string {
	switch step {
	case bisect.StepStart:
		return "session started"
	case bisect.StepGood:
		if result.Phase == bisect.PhaseExhausted {
			return "all remaining packages are good; no bad package found"
		}
		if result.Marked != nil {
			return fmt.Sprintf("marked %s good", result.Marked.Label())
		}
	case bisect.StepBad:
		if result.Phase == bisect.PhaseFound {
			return "found bad package"
		}
		if result.Marked != nil {
			return fmt.Sprintf("marked %s bad", result.Marked.Label())
		}
	case bisect.StepReset:
		return "session reset"
	}
	return "unknown transition"
}

// bisectStatusMessage names the observed session state.
func bisectStatusMessage(result bisect.Result) string {
	if result.Pending != "" {
		return fmt.Sprintf("session interrupted during %s", result.Pending)
	}
	switch result.Phase {
	case bisect.PhaseFound:
		return "session complete: bad package identified"
	case bisect.PhaseExhausted:
		return "session complete: no bad package found"
	default:
		return "session active"
	}
}

func actionBisectStart(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	ctx, stop := CommandContext(cmd)
	defer stop()

	result, err := bisect.Start(
		ctx,
		inv.Directory,
		inv.Logger,
		deps.Operations,
		deps.Router.ResolveArtifactByHash,
	)
	warnings := reportBisectFindings(cmd, inv, result.Findings)
	if err != nil {
		return err
	}
	if result.NoCandidates {
		return outputBisect(
			cmd,
			inv,
			bisectOutput{
				Message:  "no enabled package requirements found in this manifest",
				Complete: true,
				Warnings: warnings,
			},
		)
	}
	result.Warnings = append(warnings, result.Warnings...)
	output := bisectOutputFrom(result)
	output.Message = bisectTransitionMessage(bisect.StepStart, result)
	return outputBisect(cmd, inv, output)
}

func actionBisectGood(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	ctx, stop := CommandContext(cmd)
	defer stop()

	result, err := bisect.Good(
		ctx,
		inv.Directory,
		inv.Logger,
		deps.Operations,
	)
	if err != nil {
		return err
	}
	output := bisectOutputFrom(result)
	output.Message = bisectTransitionMessage(bisect.StepGood, result)
	return outputBisect(cmd, inv, output)
}

func actionBisectBad(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	ctx, stop := CommandContext(cmd)
	defer stop()

	result, err := bisect.Bad(
		ctx,
		inv.Directory,
		inv.Logger,
		deps.Operations,
	)
	if err != nil {
		return err
	}
	output := bisectOutputFrom(result)
	output.Message = bisectTransitionMessage(bisect.StepBad, result)
	return outputBisect(cmd, inv, output)
}

func actionBisectStatus(
	cmd *cobra.Command,
	inv *Invocation,
	_ *Dependencies,
	_ []string,
) error {
	ctx, stop := CommandContext(cmd)
	defer stop()

	result, err := bisect.Status(ctx, inv.Directory)
	if err != nil {
		return err
	}
	output := bisectOutputFrom(result)
	output.Message = bisectStatusMessage(result)
	return outputBisect(cmd, inv, output)
}

func actionBisectReset(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	ctx, stop := CommandContext(cmd)
	defer stop()

	result, err := bisect.Reset(
		ctx,
		inv.Directory,
		inv.Logger,
		deps.Operations,
	)
	if err != nil {
		return err
	}
	output := bisectOutputFrom(result)
	output.Message = "session reset"
	return outputBisect(cmd, inv, output)
}
