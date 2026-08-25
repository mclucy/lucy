package init

import (
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/create"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/workspace"
	"github.com/spf13/cobra"
)

const (
	flagInitAllowEmptyName = "allow-empty"
	flagInitForceName      = "force"
	flagInitWorkDirName    = "work-dir"
	flagInitGameVersion    = "game-version"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a Lucy manifest for an existing server",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionInit),
}

// NewCommand wires and returns the `lucy init` command.
func NewCommand() *cobra.Command {
	initCmd.Flags().BoolP(
		flagInitForceName,
		"f",
		false,
		"Overwrite an existing manifest without asking",
	)
	initCmd.Flags().BoolP(
		flagInitAllowEmptyName,
		"y",
		false,
		"Record an empty server when none is detected; skips the create prompt",
	)
	initCmd.Flags().String(
		flagInitGameVersion,
		"",
		"Game version recorded when no server is detected (e.g. 1.21.4)",
	)
	initCmd.Flags().String(
		flagInitWorkDirName,
		"",
		"Override working directory (for testing)",
	)
	_ = initCmd.Flags().MarkHidden(flagInitWorkDirName)
	return initCmd
}

func actionInit(cmd *cobra.Command, _ []string) error {
	workDir, err := resolveWorkDir(cmd)
	if err != nil {
		return err
	}

	opts := Options{
		GameVersion: flagString(cmd, flagInitGameVersion),
		Force:       flagBool(cmd, flagInitForceName),
		AllowEmpty:  flagBool(cmd, flagInitAllowEmptyName),
	}

	ws := workspace.NewAt(workDir)

	// An unresolved server fails the init process
	_, resolved := ws.Probe.Single()
	switch {
	case ws.Probe.HasAmbiguity():
		return fmt.Errorf(
			"cannot initialize %s: conflicting server files",
			workDir,
		)
	case !resolved && len(ws.Probe.Unidentified) > 0:
		return fmt.Errorf(
			"cannot initialize %s: unrecognized server files",
			workDir,
		)
	}

	if ManifestExists(workDir) && !opts.Force {
		rebuild, err := confirm(
			"Existing Lucy manifest found",
			fmt.Sprintf(
				"Delete %s and rebuild it from the detected server?",
				state.ManifestFile,
			),
		)
		if err != nil {
			return fmt.Errorf("rebuild prompt: %w", err)
		}
		if !rebuild {
			log.ShowInfo("Init cancelled.")
			return nil
		}
	}
	manifest, err := createManifest(ws, workDir, opts)
	if err != nil {
		return err
	}
	if manifest == nil {
		return nil
	}

	if !opts.AllowEmpty || resolved {
		approved, err := confirmManifestWrite(manifest)
		if err != nil {
			return fmt.Errorf("review step: %w", err)
		}
		if !approved {
			log.ShowInfo("Init cancelled.")
			return nil
		}
	}

	if err := SaveManifest(workDir, manifest); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	log.ReportInfo(fmt.Sprintf(
		"Wrote %s in %s",
		state.ManifestFile,
		workDir,
	))
	return nil
}

// createManifest picks the manifest content for the probe result.
// A nil manifest means the redirect ran. Init then stops.
func createManifest(
	ws workspace.Workspace,
	workDir string,
	opts Options,
) (*state.Manifest, error) {
	if _, ok := ws.Probe.Single(); ok {
		return ManifestFromDetection(ws), nil
	}

	if !opts.AllowEmpty {
		if !term.IsTerminal(os.Stdin.Fd()) {
			return nil, fmt.Errorf(
				"--allow-empty is required when stdin is not a terminal",
			)
		}
		runCreate, err := confirm(
			"No server detected in this directory",
			"Run lucy create to record one instead?",
		)
		if err != nil {
			return nil, fmt.Errorf("create prompt: %w", err)
		}
		if runCreate {
			// The redirect runs the real create flow.
			if err := create.Execute(workDir, opts.Force, "", ""); err != nil {
				return nil, fmt.Errorf("lucy create: %w", err)
			}
			return nil, nil
		}
	}

	return EmptyServerManifest(opts.GameVersion), nil
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

func flagString(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func flagBool(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}
