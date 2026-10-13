package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/message"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

const (
	flagInitAllowEmptyName  = "allow-empty"
	flagInitForceName       = "force"
	flagInitWorkDirName     = "work-dir"
	flagInitMinecraftName   = "minecraft"
	flagInitGameVersionName = "game-version"
	flagInitCoreName        = "core"
	flagInitCoreVersionName = "core-version"
	flagInitRepositoryName  = "repository"
	flagInitMCDRVersionName = "mcdr-version"
	flagInitDirectoryName   = "directory"
	flagInitDetectName      = "detect"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: message.InitShort,
	Args:  cobra.NoArgs,
	RunE:  actionInit,
}

func NewInitCommand() *cobra.Command {
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
		"Record an empty server when none is detected",
	)
	initCmd.Flags().String(
		flagInitMinecraftName,
		"",
		"Target Minecraft version (e.g. 1.21.4)",
	)
	initCmd.Flags().String(
		flagInitGameVersionName,
		"",
		"Alias for --minecraft",
	)
	initCmd.Flags().String(
		flagInitCoreName,
		"",
		"Core distribution or core@version (e.g. fabric@0.16.9, paper)",
	)
	initCmd.Flags().String(
		flagInitCoreVersionName,
		"",
		"Core distribution version or selector",
	)
	initCmd.Flags().String(
		flagInitRepositoryName,
		"",
		"GitHub repository serving the core jar (owner/repo)",
	)
	initCmd.Flags().String(
		flagInitMCDRVersionName,
		"",
		"MCDReforged version to record",
	)
	initCmd.Flags().StringP(
		flagInitDirectoryName,
		"C",
		"",
		"Workspace-relative directory that holds the server",
	)
	initCmd.Flags().Bool(
		flagInitDetectName,
		true,
		"Detect runtime from server directory",
	)
	initCmd.Flags().String(
		flagInitWorkDirName,
		"",
		"Override working directory (for testing)",
	)
	_ = initCmd.Flags().MarkHidden(flagInitWorkDirName)
	return initCmd
}

func parseCoreDistributionInit(raw string) (distribution, version string) {
	if raw == "" {
		return "", ""
	}
	if dist, ver, found := strings.Cut(raw, "@"); found {
		return dist, ver
	}
	return raw, ""
}

func actionInit(cmd *cobra.Command, _ []string) error {
	workDir, err := resolveWorkDirInit(cmd)
	if err != nil {
		return err
	}

	mc := flagStringInit(cmd, flagInitMinecraftName)
	if mc == "" {
		mc = flagStringInit(cmd, flagInitGameVersionName)
	}

	coreRaw := flagStringInit(cmd, flagInitCoreName)
	distribution, coreVer := parseCoreDistributionInit(coreRaw)
	if explicitVer := flagStringInit(cmd, flagInitCoreVersionName); explicitVer != "" {
		coreVer = explicitVer
	}

	detect := true
	if cmd.Flags().Changed(flagInitDetectName) {
		detect = flagBoolInit(cmd, flagInitDetectName)
	}

	opts := install.InitOptions{
		Minecraft:       mc,
		Distribution:    distribution,
		CoreVersion:     coreVer,
		Repository:      flagStringInit(cmd, flagInitRepositoryName),
		MCDRVersion:     flagStringInit(cmd, flagInitMCDRVersionName),
		Directory:       flagStringInit(cmd, flagInitDirectoryName),
		Force:           flagBoolInit(cmd, flagInitForceName),
		AllowEmpty:      flagBoolInit(cmd, flagInitAllowEmptyName),
		Detect:          detect,
		PromptMinecraft: promptMinecraft(cmd),
	}

	return initializeWorkspace(cmd, workDir, opts)
}

// initializeWorkspace initializes one workspace and owns its presentation.
// Warnings observed during initialization surface on stderr once, failures
// return to the command framework, and success reprints the stored manifest
// from the returned document instead of rereading it.
func initializeWorkspace(cmd *cobra.Command, workDir string, opts install.InitOptions) error {
	d, warnings, err := install.Initialize(cmd.Context(), workDir, opts)
	for _, warning := range warnings {
		report.Warn(warning)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s for Minecraft %s (%s)\n", manifest.Filename, d.Server.Minecraft, d.Server.Core.Distribution)
	return nil
}

// promptMinecraft returns the interactive Minecraft-version prompt for
// Initialize. It requires a terminal stdin with interactive mode enabled,
// echoes the question on stderr, and returns the trimmed answer.
func promptMinecraft(cmd *cobra.Command) func() (string, error) {
	return func() (string, error) {
		stdin := cmd.InOrStdin()
		file, isFile := stdin.(*os.File)
		if !isFile || !term.IsTerminal(file.Fd()) || !style.InteractiveEnabled() {
			return "", errors.New("a concrete --minecraft version is required")
		}
		fmt.Fprint(cmd.ErrOrStderr(), "Minecraft version: ")
		// Piped answers may end without a newline, so a final line behind
		// io.EOF is still a valid answer.
		text, err := bufio.NewReader(stdin).ReadString('\n')
		if err == io.EOF && strings.TrimSpace(text) != "" {
			err = nil
		}
		if err != nil {
			return "", err
		}
		answer := strings.TrimSpace(text)
		if answer == "" {
			return "", errors.New("a concrete --minecraft version is required")
		}
		return answer, nil
	}
}

func resolveWorkDirInit(cmd *cobra.Command) (string, error) {
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

func flagStringInit(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func flagBoolInit(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}
