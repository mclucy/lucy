package cmd

import (
	"fmt"
	"os"

	lucyinit "github.com/mclucy/lucy/cmd/init"
	"github.com/mclucy/lucy/state"
	"github.com/spf13/cobra"
)

const (
	flagInitYesName      = "yes"
	flagInitConflictName = "conflict"
	flagInitWorkDirName  = "work-dir"
	flagInitGameVersion  = "game-version"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Take over the current server into Lucy state",
	Long: `Initialize Lucy in the current
directory. Creates lucy.yaml (manifest + optional config overrides) and lucy-lock.yaml (resolved graph) in the project root.

Init is optimized for taking over an existing server before it behaves like a
blank-slate scaffold. Lucy reconstructs the current reality first, then draws
its managed boundary around the parts the operator wants it to own. It inspects
the live server first, records a soft manifest intent from those facts, and
writes an exact lockfile for the resolved managed state.

No files are written until you confirm at the final review step. That confirmation
is mandatory before Lucy persists intent. Existing Lucy state is preserved by
default, and takeover-style init will show you what is already on disk as an
advisory hint before you decide what Lucy should manage. Lucy absorbs the
existing server into a managed boundary instead of claiming total ownership of
the directory.

Version hints are best-effort: omit a version to use "compatible" (newest
version that appears to fit the current environment), use @latest to request
the newest available, or keep the inferred runtime version when you want
Lucy to match the current environment.`,
	RunE: runWithErrorLogging(actionInit),
}

func init() {
	initCmd.Flags().BoolP(flagInitYesName, "y", false, "Non-interactive mode: accept all defaults without prompting")
	initCmd.Flags().StringP(flagInitConflictName, "c", "preserve", "Conflict mode for existing files: preserve, abort, overwrite")
	initCmd.Flags().String(flagInitWorkDirName, "", "Override working directory (for testing)")
	initCmd.Flags().String(flagInitGameVersion, "1.21", "Game version for non-interactive init (e.g., 1.21.4)")
	_ = initCmd.Flags().MarkHidden(flagInitWorkDirName)
	rootCmd.AddCommand(initCmd)
}

func actionInit(cmd *cobra.Command, _ []string) error {
	workDir, err := resolveWorkDir(cmd)
	if err != nil {
		return err
	}

	conflictStr, _ := cmd.Flags().GetString(flagInitConflictName)
	conflictMode, err := parseConflictMode(conflictStr)
	if err != nil {
		return err
	}

	yes, _ := cmd.Flags().GetBool(flagInitYesName)
	gameVersion, _ := cmd.Flags().GetString(flagInitGameVersion)

	flowState := lucyinit.NewInitFlowState(workDir)
	flowState.ConflictResolution = conflictMode

	if gameVersion != "" && gameVersion != "1.21" && flowState.GameVersion == "" {
		flowState.GameVersion = gameVersion
	}

	if yes {
		return runNonInteractiveInit(workDir, flowState)
	}
	return runInteractiveInit(workDir, flowState)
}

func resolveWorkDir(cmd *cobra.Command) (string, error) {
	override, _ := cmd.Flags().GetString(flagInitWorkDirName)
	if override != "" {
		return override, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not determine working directory: %w", err)
	}
	return wd, nil
}

func parseConflictMode(s string) (lucyinit.ConflictMode, error) {
	switch s {
	case "preserve", "":
		return lucyinit.PreserveExisting, nil
	case "abort":
		return lucyinit.AbortOnConflict, nil
	case "overwrite":
		return lucyinit.OverwriteAll, nil
	default:
		return "", fmt.Errorf("unknown conflict mode %q: must be preserve, abort, or overwrite", s)
	}
}

func runNonInteractiveInit(workDir string, s *lucyinit.InitFlowState) error {
	if s.GameVersion == "" {
		s.GameVersion = "1.21"
	}
	if s.Platform == "" {
		s.Platform = "none"
	}
	if s.Platform == "none" && s.PlatformVersion == "" {
		s.PlatformVersion = "none"
	}

	if !lucyinit.CanProceed(s) {
		return fmt.Errorf("cannot proceed: managed roots are required for non-interactive init (run interactively or provide explicit roots)")
	}
	s.Confirmed = true
	return writeInitResult(workDir, s)
}

func runInteractiveInit(workDir string, s *lucyinit.InitFlowState) error {
	if err := lucyinit.RunInteractiveInit(s); err != nil {
		return fmt.Errorf("init flow: %w", err)
	}
	if s.Aborted {
		fmt.Fprintln(os.Stderr, "Init cancelled.")
		return nil
	}
	if !s.Confirmed {
		fmt.Fprintln(os.Stderr, "Init cancelled.")
		return nil
	}
	return writeInitResult(workDir, s)
}

func writeInitResult(workDir string, s *lucyinit.InitFlowState) error {
	result, err := lucyinit.BuildResult(s)
	if err != nil {
		return fmt.Errorf("build init plan: %w", err)
	}

	if result.ConfigToWrite != nil {
		if err := state.WriteConfig(workDir, result.ConfigToWrite); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
	}
	if result.ManifestToWrite != nil {
		if err := state.WriteManifest(workDir, result.ManifestToWrite); err != nil {
			return fmt.Errorf("write manifest: %w", err)
		}
	}
	if result.LockToWrite != nil {
		if err := state.WriteLock(workDir, result.LockToWrite); err != nil {
			return fmt.Errorf("write lock: %w", err)
		}
	}
	lucyinit.RefreshObservedStateAfterInitWrites(workDir)

	printInitSummary(result)
	return nil
}

func printInitSummary(result lucyinit.InitFlowResult) {
	fmt.Println("\nLucy initialized successfully.")
	if len(result.WrittenFiles) > 0 {
		fmt.Println("\nFiles written:")
		for _, f := range result.WrittenFiles {
			fmt.Printf("  %s\n", f)
		}
	}
	if len(result.SkippedFiles) > 0 {
		fmt.Println("\nFiles preserved (already exist):")
		for _, f := range result.SkippedFiles {
			fmt.Printf("  %s\n", f)
		}
	}
}
