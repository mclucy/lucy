package init

import (
	"fmt"
	"os"
	"strings"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli"
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
	Short: "Generate a manifest file for an existing server directory",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionInit),
}

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
	cli.AddNoStyleFlag(initCmd)
	return initCmd
}

func parseCoreDistribution(raw string) (distribution, version string) {
	if raw == "" {
		return "", ""
	}
	if dist, ver, found := strings.Cut(raw, "@"); found {
		return dist, ver
	}
	return raw, ""
}

func actionInit(cmd *cobra.Command, _ []string) error {
	workDir, err := resolveWorkDir(cmd)
	if err != nil {
		return err
	}

	mc := flagString(cmd, flagInitMinecraftName)
	if mc == "" {
		mc = flagString(cmd, flagInitGameVersionName)
	}

	coreRaw := flagString(cmd, flagInitCoreName)
	distribution, coreVer := parseCoreDistribution(coreRaw)
	if explicitVer := flagString(cmd, flagInitCoreVersionName); explicitVer != "" {
		coreVer = explicitVer
	}

	detect := true
	if cmd.Flags().Changed(flagInitDetectName) {
		detect = flagBool(cmd, flagInitDetectName)
	}

	opts := install.InitOptions{
		Minecraft:    mc,
		Distribution: distribution,
		CoreVersion:  coreVer,
		Repository:   flagString(cmd, flagInitRepositoryName),
		MCDRVersion:  flagString(cmd, flagInitMCDRVersionName),
		Directory:    flagString(cmd, flagInitDirectoryName),
		Force:        flagBool(cmd, flagInitForceName),
		AllowEmpty:   flagBool(cmd, flagInitAllowEmptyName),
		Detect:       detect,
	}

	return install.Initialize(cmd.Context(), workDir, opts)
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
