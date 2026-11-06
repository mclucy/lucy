package cli

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/spf13/cobra"
)

const (
	flagWithOptionalName = "with-optional"
	flagLockedName       = "locked"
	flagOfflineName      = "offline"
	flagDowngradeName    = "downgrade"
	flagForceName        = "force"
)

func NewAddCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add",
		Short:   help.AddShort,
		Args:    cobra.MinimumNArgs(1),
		Aliases: []string{"a"},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			ecosystem, _ := cmd.Flags().GetString(FlagEcosystem)
			if ecosystem != "" && !catalog.Ecosystem(ecosystem).Valid() {
				return fmt.Errorf("invalid ecosystem %q", ecosystem)
			}
			return validateAddDowngradeFlags(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionAdd(cmd, inv, deps, args)
		},
	}
	installJSON.Bind(contracts, cmd)
	cmd.Flags().Bool(
		flagWithOptionalName,
		false,
		"Also install optional upstream dependencies",
	)
	cmd.Flags().Bool(
		flagLockedName,
		false,
		"Do not resolve new versions; fail when the lock cannot satisfy",
	)
	cmd.Flags().Bool(
		flagOfflineName,
		false,
		"Disallow network access; fail if a needed file is not already available locally",
	)
	cmd.Flags().Int(
		flagDowngradeName,
		0,
		"Step back N versions from the locked version; --downgrade=N, default 1",
	)
	cmd.Flags().Lookup(flagDowngradeName).NoOptDefVal = "1"
	cmd.Flags().Bool(
		flagForceName,
		false,
		"Commit the downgrade despite package conflicts (requires --downgrade)",
	)
	AddEcosystemFlag(cmd)
	return cmd
}

func validateAddDowngradeFlags(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool(flagForceName)
	if !cmd.Flags().Changed(flagDowngradeName) {
		if force {
			return fmt.Errorf("--%s requires --%s", flagForceName, flagDowngradeName)
		}
		return nil
	}

	steps, _ := cmd.Flags().GetInt(flagDowngradeName)
	if steps < 1 {
		return fmt.Errorf("--%s needs a positive step count", flagDowngradeName)
	}
	if locked, _ := cmd.Flags().GetBool(flagLockedName); locked {
		return fmt.Errorf(
			"--%s cannot be combined with --%s",
			flagDowngradeName,
			flagLockedName,
		)
	}
	if offline, _ := cmd.Flags().GetBool(flagOfflineName); offline {
		return fmt.Errorf(
			"--%s cannot be combined with --%s",
			flagDowngradeName,
			flagOfflineName,
		)
	}
	for _, raw := range args {
		if strings.ContainsRune(raw, '@') {
			return fmt.Errorf(
				"--%s does not accept a version selector: %q",
				flagDowngradeName,
				raw,
			)
		}
	}
	return nil
}

func actionAdd(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	args []string,
) error {
	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
	locked, _ := cmd.Flags().GetBool(flagLockedName)
	offline, _ := cmd.Flags().GetBool(flagOfflineName)
	force, _ := cmd.Flags().GetBool(flagForceName)
	downgrade := cmd.Flags().Changed(flagDowngradeName)
	steps, _ := cmd.Flags().GetInt(flagDowngradeName)

	ecosystem, _ := cmd.Flags().GetString(FlagEcosystem)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    withOptional,
		PublishManifest: false,
	}

	output := newInstallOutput(cmd, inv)
	defer output.Close()
	opts.Trace = output.Trace()
	ctx, stop := CommandContext(cmd)
	defer stop()
	var (
		result *install.Result
		err    error
	)
	if downgrade {
		result, err = deps.Operations.Downgrade(
			ctx,
			inv.Directory,
			args,
			catalog.Ecosystem(ecosystem),
			operation.DowngradeOptions{
				Steps:                  steps,
				AcceptPackageConflicts: force,
				Install:                opts,
			},
		)
	} else {
		result, err = deps.Operations.Add(
			ctx,
			inv.Directory,
			args,
			catalog.Ecosystem(ecosystem),
			opts,
		)
	}
	if err != nil {
		return output.Failure(err)
	}
	return output.Render(result)
}
