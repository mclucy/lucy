package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove packages from Lucy-managed intent",
	Args:  cobra.MinimumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return CompletePackageIDSuggestions(context.Background(), "remove", toComplete)
	},
	RunE: runWithErrorLogging(actionRemove),
}

func init() {
	addNoStyleFlag(removeCmd)
	rootCmd.AddCommand(removeCmd)
}

func actionRemove(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	hasLucyState, err := lucyStateDirExists(workDir)
	if err != nil {
		return err
	}
	if !hasLucyState {
		return fmt.Errorf("lucy state is not initialized")
	}

	stateSvc := state.NewProjectStateService(workDir)
	if err := stateSvc.Load(cmd.Context()); err != nil {
		return fmt.Errorf("load lucy state: %w", err)
	}
	if stateSvc.Manifest() == nil {
		return fmt.Errorf("manifest is required for remove")
	}

	ids := make([]types.PackageId, 0, len(args))
	for _, arg := range args {
		id, err := syntax.Parse(arg)
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}

	manifest := state.UpdateManifestRolesForRemove(stateSvc.Manifest(), ids, stateSvc.Lock())
	if err := state.WriteManifest(workDir, manifest); err != nil {
		return fmt.Errorf("update manifest: %w", err)
	}

	if stateSvc.Lock() == nil {
		return nil
	}

	lock := state.PruneLockForManifest(stateSvc.Lock(), manifest)
	if err := state.WriteLock(workDir, lock); err != nil {
		return fmt.Errorf("update lock: %w", err)
	}

	return nil
}
