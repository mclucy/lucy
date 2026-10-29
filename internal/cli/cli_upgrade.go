package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/spf13/cobra"
)

const (
	flagUpgradeAllName         = "all"
	flagUpgradeInteractiveName = "interactive"
	flagUpgradeDryRunName      = "dry-run"
	flagUpgradePolicyName      = "policy"
)

var upgradeFrontierJSON JSONResult[upgradeFrontierDocument]

func NewUpgradeCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	upgradeCmd := &cobra.Command{
		Use:     "upgrade [packages...]",
		Short:   help.UpgradeShort,
		Aliases: []string{"up", "update"},
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionUpgrade(cmd, inv, deps, args)
		},
	}
	upgradeFrontierJSON.Bind(contracts, upgradeCmd)
	installJSON.Bind(contracts, upgradeCmd)
	upgradeCmd.Flags().BoolP(
		flagUpgradeAllName,
		"a",
		false,
		"Upgrade every enabled manifest root",
	)
	upgradeCmd.Flags().Bool(
		flagUpgradeInteractiveName,
		false,
		"Select manifest roots to upgrade",
	)
	upgradeCmd.Flags().Bool(
		flagUpgradeDryRunName,
		false,
		"Resolve and show the plan without changing files",
	)
	upgradeCmd.Flags().String(
		flagUpgradePolicyName,
		"more",
		"Upgrade policy: more, later, or options",
	)
	return upgradeCmd
}

func actionUpgrade(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	args []string,
) error {
	all, _ := cmd.Flags().GetBool(flagUpgradeAllName)
	interactive, _ := cmd.Flags().GetBool(flagUpgradeInteractiveName)
	dryRun, _ := cmd.Flags().GetBool(flagUpgradeDryRunName)
	policy, _ := cmd.Flags().GetString(flagUpgradePolicyName)
	policySet := cmd.Flags().Changed(flagUpgradePolicyName)
	noInteractive, _ := cmd.Flags().GetBool(FlagNoInteractive)
	jsonMode := JSONMode(cmd) != JSONNone

	switch {
	case all && len(args) > 0:
		return fmt.Errorf("--%s cannot be combined with package arguments", flagUpgradeAllName)
	case all && interactive:
		return fmt.Errorf("--%s cannot be combined with --%s", flagUpgradeAllName, flagUpgradeInteractiveName)
	case interactive && len(args) > 0:
		return fmt.Errorf("--%s cannot be combined with package arguments", flagUpgradeInteractiveName)
	case interactive && noInteractive:
		return fmt.Errorf("--%s cannot be combined with --%s", flagUpgradeInteractiveName, FlagNoInteractive)
	case policySet && !all:
		return fmt.Errorf("--%s applies to global upgrades; add --%s", flagUpgradePolicyName, flagUpgradeAllName)
	case policy != "more" && policy != "later" && policy != "options" && !isUpgradeHash(policy):
		return fmt.Errorf("unknown upgrade policy %q", policy)
	case !all && !interactive && len(args) == 0:
		return fmt.Errorf("specify packages, --%s, or --%s", flagUpgradeAllName, flagUpgradeInteractiveName)
	case policy == "options" && noInteractive && !dryRun && !jsonMode:
		return fmt.Errorf("--%s options needs a selection; use --%s <hash>", flagUpgradePolicyName, flagUpgradePolicyName)
	}

	ctx, stop := CommandContext(cmd)
	defer stop()
	targets := args
	if interactive {
		selected, err := promptUpgradeRoots(ctx, cmd, inv, inv.Directory)
		if err != nil {
			return err
		}
		targets = selected
	}
	output := newInstallOutput(cmd, inv)
	result, err := deps.Operations.PlanUpgrade(
		ctx,
		inv.Directory,
		operation.UpgradeOptions{
			All:     all,
			Targets: targets,
			Trace:   output.Trace(),
		},
	)
	if err != nil {
		return output.Failure(err)
	}
	output.Close()

	if jsonMode && (dryRun || policy == "options") {
		selectedHash := ""
		if policy != "options" {
			selected, err := operation.SelectUpgradeOption(result, policy)
			if err != nil {
				return err
			}
			selectedHash = selected.Hash
		}
		return upgradeFrontierJSON.Write(output.Output, upgradeFrontierDocument{
			Schema:                  1,
			Kind:                    "upgrade-frontier",
			Policy:                  policy,
			SelectedHash:            selectedHash,
			BaseManifestFingerprint: result.BaseManifestFingerprint,
			BaseLockFingerprint:     result.BaseLockFingerprint,
			Frontier:                upgradeFrontierEntries(result.Options),
			Warnings:                warningMessages(result.Warnings),
		})
	}
	if policy == "options" {
		if dryRun {
			output.RenderUpgradeFrontier(result)
			printUpgradeWarnings(output, result.Warnings)
			return nil
		}
		index, err := promptUpgradeChoice(ctx, cmd, inv, result.Options)
		if err != nil {
			return err
		}
		option := result.Options[index]
		output.RenderUpgradePlan(option, "options", false)
		printUpgradeWarnings(output, result.Warnings)
		_, err = applyUpgradeOption(ctx, cmd, inv, deps, option, "options", result.Warnings)
		return err
	}
	selected, err := operation.SelectUpgradeOption(result, policy)
	if err != nil {
		return err
	}
	if dryRun {
		output.RenderUpgradePlan(*selected, policy, true)
		printUpgradeWarnings(output, result.Warnings)
		return nil
	}
	output.RenderUpgradePlan(*selected, policy, false)
	if !selected.Plan.Result.Changed() {
		printUpgradeWarnings(output, result.Warnings)
		if jsonMode {
			document := selected.Plan.Result
			document.WarningMessages = append(warningMessages(document.Warnings), warningMessages(result.Warnings)...)
			return installJSON.Write(output.Output, document)
		}
		return nil
	}
	if !noInteractive && !jsonMode {
		if !NewPromptGate(cmd, inv).Available() {
			return fmt.Errorf("apply without a prompt with --%s", FlagNoInteractive)
		}
		confirmed, err := promptUpgradeConfirm(ctx, cmd)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	applied, err := applyUpgradeOption(ctx, cmd, inv, deps, *selected, policy, result.Warnings)
	if err != nil {
		return err
	}
	if jsonMode {
		return installJSON.Write(output.Output, applied)
	}
	return nil
}

func applyUpgradeOption(
	ctx context.Context,
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	option operation.UpgradeOption,
	policy string,
	warnings []error,
) (*install.Result, error) {
	if option.Plan != nil && option.Plan.Result != nil {
		option.Plan.Result.Warnings = append(option.Plan.Result.Warnings, warnings...)
	}
	if JSONMode(cmd) != JSONNone {
		output := newInstallOutput(cmd, inv)
		defer output.Close()
		result, err := deps.Operations.ApplyUpgrade(
			ctx,
			inv.Directory,
			option,
			policy,
			install.Options{Trace: output.Trace()},
		)
		if err != nil {
			return nil, output.Failure(err)
		}
		result.WarningMessages = warningMessages(result.Warnings)
		return result, nil
	}
	output := newInstallOutput(cmd, inv)
	defer output.Close()
	result, err := deps.Operations.ApplyUpgrade(
		ctx,
		inv.Directory,
		option,
		policy,
		install.Options{Trace: output.Trace()},
	)
	if err != nil {
		return nil, output.Failure(err)
	}
	if err := output.RenderUpgradeApplied(result, policy, option.Hash); err != nil {
		return nil, err
	}
	return result, nil
}

func promptUpgradeRoots(
	ctx context.Context,
	cmd *cobra.Command,
	inv *Invocation,
	root string,
) ([]string, error) {
	if !NewPromptGate(cmd, inv).Available() {
		return nil, errors.New("interactive selection needs a terminal; use --all or name packages")
	}
	refs, err := operation.ListUpgradeRoots(root)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, errors.New("the manifest has no enabled package roots")
	}
	options := make([]huh.Option[string], 0, len(refs))
	for _, ref := range refs {
		options = append(options, huh.NewOption(ref, ref))
	}
	var selected []string
	form := huh.NewForm(huh.NewGroup(huh.NewMultiSelect[string]().
		Title("Select packages to upgrade").
		Options(options...).
		Value(&selected))).
		WithInput(cmd.InOrStdin()).
		WithOutput(cmd.ErrOrStderr())
	if err := NormalizePromptError(ctx, form.RunWithContext(ctx)); err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, errors.New("no packages selected")
	}
	return selected, nil
}

func promptUpgradeChoice(
	ctx context.Context,
	cmd *cobra.Command,
	inv *Invocation,
	options []operation.UpgradeOption,
) (int, error) {
	if !NewPromptGate(cmd, inv).Available() {
		return 0, errors.New("interactive selection needs a terminal; use --policy <hash>")
	}
	huhOptions := make([]huh.Option[int], 0, len(options))
	for i, option := range options {
		huhOptions = append(huhOptions, huh.NewOption(
			fmt.Sprintf("%d. %s", i+1, upgradeOptionSummary(option)),
			i,
		))
	}
	selected := 0
	form := huh.NewForm(huh.NewGroup(huh.NewSelect[int]().
		Title("Select an upgrade plan").
		Options(huhOptions...).
		Value(&selected))).
		WithInput(cmd.InOrStdin()).
		WithOutput(cmd.ErrOrStderr())
	if err := NormalizePromptError(ctx, form.RunWithContext(ctx)); err != nil {
		return 0, err
	}
	return selected, nil
}

func promptUpgradeConfirm(ctx context.Context, cmd *cobra.Command) (bool, error) {
	confirmed := false
	form := huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title("Apply the planned upgrade?").
		Value(&confirmed))).
		WithInput(cmd.InOrStdin()).
		WithOutput(cmd.ErrOrStderr())
	if err := NormalizePromptError(ctx, form.RunWithContext(ctx)); err != nil {
		return false, err
	}
	return confirmed, nil
}

func isUpgradeHash(value string) bool {
	return strings.HasPrefix(value, "sha256:")
}
