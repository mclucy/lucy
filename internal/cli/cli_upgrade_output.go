package cli

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/terminal/style"
)

type upgradeFrontierDocument struct {
	Schema                  int                    `json:"schema"`
	Kind                    string                 `json:"kind"`
	Policy                  string                 `json:"policy,omitempty"`
	SelectedHash            string                 `json:"selected_hash,omitempty"`
	BaseManifestFingerprint string                 `json:"base_manifest_fingerprint"`
	BaseLockFingerprint     string                 `json:"base_lock_fingerprint"`
	Frontier                []upgradeFrontierEntry `json:"frontier"`
	Warnings                []string               `json:"warnings,omitempty"`
}

type upgradeFrontierEntry struct {
	Hash    string                   `json:"hash"`
	Metrics operation.UpgradeMetrics `json:"metrics"`
	Result  *install.Result          `json:"result"`
}

func upgradeFrontierEntries(options []operation.UpgradeOption) []upgradeFrontierEntry {
	entries := make([]upgradeFrontierEntry, 0, len(options))
	for _, option := range options {
		if option.Plan != nil && option.Plan.Result != nil {
			option.Plan.Result.PlanHash = option.Hash
		}
		entries = append(entries, upgradeFrontierEntry{
			Hash:    option.Hash,
			Metrics: option.Metrics,
			Result:  option.Plan.Result,
		})
	}
	return entries
}

func (o *installOutput) RenderUpgradeFrontier(result *operation.UpgradeResult) {
	fmt.Fprintln(o.Human(), "Upgrade options:")
	for i, option := range result.Options {
		fmt.Fprintf(o.Human(), "  %d. %s\n", i+1, upgradeOptionSummary(option))
	}
}

func (o *installOutput) RenderUpgradePlan(
	option operation.UpgradeOption,
	policy string,
	dryRun bool,
) {
	result := option.Plan.Result
	if result == nil {
		return
	}
	if !result.Changed() {
		fmt.Fprintln(o.Human(), style.Success("✓")+" Already up to date")
		return
	}
	fmt.Fprintln(o.Human(), "Plan:")
	for _, line := range packageChangeLines(result.Packages) {
		fmt.Fprintln(o.Human(), line)
	}
	for _, change := range result.ManifestChanges {
		fmt.Fprintf(
			o.Human(),
			"  %s manifest %s  %s → %s\n",
			style.Warning("~"),
			change.Reference,
			change.PreviousSelector,
			change.Selector,
		)
	}
	fmt.Fprintln(o.Human(), style.Muted(fmt.Sprintf(
		"%d roots upgraded, %d version bumps",
		option.Metrics.UpgradedRoots,
		option.Metrics.VersionBumps,
	)))
	fmt.Fprintln(o.Human(), style.Muted("Policy: "+policy))
	fmt.Fprintln(o.Human(), style.Muted("Plan: "+option.Hash))
	if dryRun {
		fmt.Fprintln(o.Human(), style.Muted("No files were changed."))
	}
}

func (o *installOutput) RenderUpgradeApplied(
	result *install.Result,
	policy string,
	hash string,
) error {
	if result == nil {
		return nil
	}
	result.Warnings = append(result.Warnings, javaWarnings(result.Java)...)
	result.WarningMessages = warningMessages(result.Warnings)
	if o.JSON() {
		return o.WriteJSON(result)
	}
	if result.MCDR != nil {
		fmt.Fprintf(
			o.Human(),
			"  %s mcdr python inputs  %d → %d\n",
			style.Warning("~"),
			result.MCDR.PreviousInputs,
			result.MCDR.Inputs,
		)
	}
	fmt.Fprintln(o.Human(), style.Success("✓")+" Upgrade applied")
	if changed := result.Files.Created + result.Files.Replaced + result.Files.Removed; changed > 0 {
		fmt.Fprintln(o.Human(), style.Muted("  "+fileCountLabel(changed)+" changed"))
	}
	fmt.Fprintln(o.Human(), style.Muted("  Policy: "+policy))
	fmt.Fprintln(o.Human(), style.Muted("  Plan: "+hash))
	if trailer := publicationLine(result); trailer != "" {
		fmt.Fprintln(o.Human(), trailer)
	}
	o.Close()
	for _, warning := range result.Warnings {
		o.Warn(warning)
	}
	return nil
}

func printUpgradeWarnings(output *installOutput, warnings []error) {
	for _, warning := range warnings {
		output.Warn(warning)
	}
}

func upgradeOptionSummary(option operation.UpgradeOption) string {
	metrics := fmt.Sprintf(
		"%d roots upgraded, %d version bumps",
		option.Metrics.UpgradedRoots,
		option.Metrics.VersionBumps,
	)
	result := option.Plan.Result
	if result == nil {
		return metrics
	}
	changes := make([]string, 0, 4)
	for _, change := range result.Packages {
		name := change.Name
		if change.Previous != "" {
			changes = append(changes, fmt.Sprintf("%s %s→%s", name, change.Previous, change.Version))
		} else {
			changes = append(changes, fmt.Sprintf("%s %s", name, change.Version))
		}
		if len(changes) == 3 {
			break
		}
	}
	if len(result.ManifestChanges) > 0 {
		changes = append(changes, fmt.Sprintf("%d manifest repins", len(result.ManifestChanges)))
	}
	if len(changes) == 0 {
		changes = append(changes, "no package changes")
	}
	return strings.Join(changes, ", ") + " (" + metrics + ")"
}
