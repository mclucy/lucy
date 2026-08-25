package create

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/workspace"
	"github.com/spf13/cobra"
)

const (
	flagCreateForceName   = "force"
	flagCreateCoreName    = "core"
	flagCreateMcName      = "minecraft"
	flagCreateWorkDirName = "work-dir"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a lucy.yaml",
	Long: `Create a lucy.yaml.

To generate a lucy.yaml for an existing server, use lucy init instead. The command
is interactive by default; pass --minecraft, and optionally --core, for non-interactive
usage. Leaving out the cores records a vanilla server.`,
	Args: cobra.NoArgs,
	RunE: cli.WithErrorLogging(actionCreate),
}

// NewCommand wires and returns the `lucy create` command.
func NewCommand() *cobra.Command {
	createCmd.Flags().BoolP(
		flagCreateForceName,
		"f",
		false,
		"Bypass directory warnings without asking",
	)
	createCmd.Flags().StringP(
		flagCreateCoreName,
		"c",
		"",
		"Server cores (e.g. fabric@0.16.9,mcdr)",
	)
	createCmd.Flags().StringP(
		flagCreateMcName,
		"m",
		"",
		"Target Minecraft version (e.g. 1.21.4)",
	)
	createCmd.Flags().String(
		flagCreateWorkDirName,
		"",
		"Override working directory (for testing)",
	)
	_ = createCmd.Flags().MarkHidden(flagCreateWorkDirName)
	return createCmd
}

func actionCreate(cmd *cobra.Command, _ []string) error {
	workDir, err := resolveWorkDir(cmd)
	if err != nil {
		return err
	}
	return Execute(
		workDir,
		flagBool(cmd, flagCreateForceName),
		flagString(cmd, flagCreateCoreName),
		flagString(cmd, flagCreateMcName),
	)
}

// Execute is the main entrance
func Execute(
	workDir string,
	force bool,
	rawCoresFlag, gameVersionFlag string,
) error {
	rawCores, gameVersion, cancelled, err := gatherInputs(
		rawCoresFlag,
		gameVersionFlag,
	)
	if err != nil {
		return err
	}
	if cancelled {
		log.ShowInfo("Create cancelled.")
		return nil
	}

	cores, err := parseCores(rawCores)
	if err != nil {
		return err
	}

	ws := workspace.NewAt(workDir)
	proceed, err := admitDirectory(workDir, ws, force)
	if err != nil || !proceed {
		return err
	}

	// TODO: Download and bootstrap the recorded cores here once the
	// creation pipeline exists. Until then create only records intent.
	manifest := manifestForCores(gameVersion, cores)
	service := state.NewProjectStateService(workDir)
	if err := service.Save(context.Background(), manifest, nil); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}

	log.ReportInfo(fmt.Sprintf(
		"Recorded server intent for Minecraft %s in %s",
		gameVersion,
		workDir,
	))
	log.ShowInfo(
		"The server itself is not downloaded yet; core installation arrives with the full creation flow.",
	)
	return nil
}

// admitDirectory enforces creation rules:
//
//   - Cannot have an existing server, with manifest or not
//   - Ask to proceed a server is suspected
//   - Ask to overwrite when there's a manifest file but no server
func admitDirectory(
	workDir string,
	ws workspace.Workspace,
	force bool,
) (bool, error) {
	if _, ok := ws.Probe.Single(); ok {
		return false, fmt.Errorf(
			"a server exists in %s; use lucy init instead to generate a lucy.yaml",
			workDir,
		)
	}

	// An existing manifest without a server is a stale or handwritten
	// record. Replacing it needs consent; --force gives it silently.
	if _, err := os.Stat(filepath.Join(workDir, string(state.ManifestFile))); err == nil {
		replace, err := confirmProceed(
			"Found existing lucy.yaml",
			fmt.Sprintf(
				"%s already has a lucy.yaml. Overwrite it?",
				workDir,
			),
			force,
		)
		if err != nil || !replace {
			return false, err
		}
	}

	switch {
	case ws.Probe.HasAmbiguity():
		return confirmProceed(
			"Conflicting server files detected",
			fmt.Sprintf(
				"%s looks like a broken server installation. Create a new server here anyway?",
				workDir,
			),
			force,
		)
	case len(ws.Probe.Unidentified) > 0:
		return confirmProceed(
			"Unrecognized server files detected",
			fmt.Sprintf(
				"%s looks like a broken server installation. Create a new server here anyway?",
				workDir,
			),
			force,
		)
	}

	nonEmpty, err := dirHasEntries(workDir)
	if err != nil {
		return false, err
	}
	if nonEmpty {
		return confirmProceed(
			"Directory is not empty",
			fmt.Sprintf(
				"%s holds files but no recognizable server. Create a new server here anyway?",
				workDir,
			),
			force,
		)
	}
	return true, nil
}

func dirHasEntries(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read directory %s: %w", dir, err)
	}
	return len(entries) > 0, nil
}

func resolveWorkDir(cmd *cobra.Command) (string, error) {
	override, _ := cmd.Flags().GetString(flagCreateWorkDirName)
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
