package add

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

const (
	flagForceName        = "force"
	flagWithOptionalName = "with-optional"
	flagNoOptionalName   = "no-optional"
	flagPlatformName     = cli.FlagPlatform
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add packages under explicit operator control",
	Args:  cobra.MinimumNArgs(1),
	ValidArgsFunction: func(
		cmd *cobra.Command,
		args []string,
		toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		return cli.CompletePackageIDSuggestions(
			context.Background(),
			"add",
			toComplete,
		)
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
		noOptional, _ := cmd.Flags().GetBool(flagNoOptionalName)
		if withOptional && noOptional {
			return fmt.Errorf("--with-optional and --no-optional cannot be used together")
		}
		platform, _ := cmd.Flags().GetString(flagPlatformName)
		if platform != "" && !types.Ecosystem(platform).Valid() {
			return fmt.Errorf("--platform must name a supported ecosystem")
		}
		return nil
	},
	RunE: cli.WithErrorLogging(actionAdd),
}

// NewCommand wires and returns the `lucy add` command.
func NewCommand() *cobra.Command {
	addCmd.Flags().BoolP(
		flagForceName,
		"f",
		false,
		"Ignore version, dependency, and platform warnings",
	)
	addCmd.Flags().Bool(
		flagWithOptionalName,
		false,
		"Also install optional upstream dependencies",
	)
	addCmd.Flags().Bool(
		flagNoOptionalName,
		false,
		"Skip optional upstream dependencies (default)",
	)
	cli.AddPlatformFlag(addCmd)
	cli.AddNoStyleFlag(addCmd)
	return addCmd
}

func actionAdd(cmd *cobra.Command, args []string) error {
	ws, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("unable to get current directory: %w", err)
	}

	stateSvc := state.NewProjectStateService(ws)
	hasLucyState, err := cli.LucyStateDirExists(ws)
	if err != nil {
		return err
	}
	if hasLucyState {
		if err := stateSvc.Load(cmd.Context()); err != nil {
			return fmt.Errorf("load lucy state: %w", err)
		}
		log.ShowInfo(formatStateSummary(stateSvc))
	}

	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
	force, _ := cmd.Flags().GetBool(flagForceName)

	options := install.DefaultOptions()
	options.WithOptional = withOptional
	options.Force = force
	options.UseGitHubMirror, _ = cmd.Flags().GetBool(cli.FlagUseGitHubMirror)

	platformArg, _ := cmd.Flags().GetString(flagPlatformName)
	platform := types.Ecosystem(platformArg)
	requests := make([]types.PackageRequest, 0, len(args))
	for _, arg := range args {
		req, err := packageRequestFromInput(arg)
		if err != nil {
			return fmt.Errorf("stopping package addition: %w", err)
		}
		if platform != types.EcoUnspecified {
			req.Eco = platform
		}
		requests = append(requests, req)
	}

	var result *install.Result
	if len(requests) > 1 {
		result, err = install.InstallMany(cmd.Context(), requests, options)
	} else {
		req := requests[0]
		result, err = install.Install(cmd.Context(), req, options)
	}
	if err != nil {
		if conflictErr, ok := errors.AsType[*resolve.ConstraintConflictError](err); ok {
			return cli.FormatConstraintConflict(conflictErr)
		}
		return err
	}

	if !hasLucyState {
		return nil
	}

	if err := updateAddState(
		cmd.Context(),
		ws,
		stateSvc,
		requests,
		result,
	); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}

func formatStateSummary(stateSvc *state.ProjectStateService) string {
	status := []string{
		presenceLabel(
			"config",
			stateSvc.Manifest() != nil && stateSvc.Manifest().Config != nil,
		),
		presenceLabel("manifest", stateSvc.Manifest() != nil),
		presenceLabel("lock", stateSvc.Lock() != nil),
	}
	return "Lucy state: " + strings.Join(status, ", ")
}

func presenceLabel(name string, present bool) string {
	if present {
		return name + " present"
	}
	return name + " absent"
}

func updateAddState(
	ctx context.Context,
	workDir string,
	stateSvc *state.ProjectStateService,
	requests []types.PackageRequest,
	result *install.Result,
) error {
	if stateSvc == nil {
		return nil
	}

	manifestIntent := buildUpdatedManifest(stateSvc.Manifest(), requests)
	if result == nil || len(result.Installed) == 0 {
		return stateSvc.Save(ctx, manifestIntent, nil)
	}

	lock := cli.BuildUpdatedLock(workDir, manifestIntent, stateSvc.Lock(), result)
	manifest := state.UpdateManifestRolesForAdd(
		stateSvc.Manifest(),
		requests,
		lock,
	)
	return stateSvc.Save(ctx, manifest, lock)
}

func buildUpdatedManifest(
	existing *state.Manifest,
	requests []types.PackageRequest,
) *state.Manifest {
	manifest := existing
	for _, req := range requests {
		manifest = state.UpsertManifestRequiredIntent(
			manifest,
			req,
			req.Source.String(),
		)
	}
	return manifest
}
