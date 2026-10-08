package create

import (
	"fmt"
	"os"
	"strings"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/spf13/cobra"
)

const (
	flagCreateForceName       = "force"
	flagCreateCoreName        = "core"
	flagCreateCoreVersionName = "core-version"
	flagCreateMcName          = "minecraft"
	flagCreateGameVersionName = "game-version"
	flagCreateRepositoryName  = "repository"
	flagCreateMCDRVersionName = "mcdr-version"
	flagCreateDirectoryName   = "directory"
	flagCreateWorkDirName     = "work-dir"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a lucy.yaml",
	Args:  cobra.NoArgs,
	RunE:  cli.WithErrorLogging(actionCreate),
}

func NewCommand() *cobra.Command {
	createCmd.Flags().BoolP(
		flagCreateForceName,
		"f",
		false,
		"Overwrite an existing manifest without asking",
	)
	createCmd.Flags().StringP(
		flagCreateCoreName,
		"c",
		"",
		"Core distribution or core@version (e.g. fabric@0.16.9, paper)",
	)
	createCmd.Flags().String(
		flagCreateCoreVersionName,
		"",
		"Core distribution version or selector",
	)
	createCmd.Flags().StringP(
		flagCreateMcName,
		"m",
		"",
		"Target Minecraft version (e.g. 1.21.4)",
	)
	createCmd.Flags().String(
		flagCreateGameVersionName,
		"",
		"Alias for --minecraft",
	)
	createCmd.Flags().String(
		flagCreateRepositoryName,
		"",
		"GitHub repository serving the core jar (owner/repo)",
	)
	createCmd.Flags().String(
		flagCreateMCDRVersionName,
		"",
		"MCDReforged version to record",
	)
	createCmd.Flags().StringP(
		flagCreateDirectoryName,
		"C",
		"",
		"Workspace-relative directory that holds the server",
	)
	createCmd.Flags().String(
		flagCreateWorkDirName,
		"",
		"Override working directory (for testing)",
	)
	_ = createCmd.Flags().MarkHidden(flagCreateWorkDirName)
	cli.AddNoStyleFlag(createCmd)
	return createCmd
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

func actionCreate(cmd *cobra.Command, _ []string) error {
	workDir, err := resolveWorkDir(cmd)
	if err != nil {
		return err
	}

	mc := flagString(cmd, flagCreateMcName)
	if mc == "" {
		mc = flagString(cmd, flagCreateGameVersionName)
	}

	coreRaw := flagString(cmd, flagCreateCoreName)
	distribution, coreVer := parseCoreDistribution(coreRaw)
	if explicitVer := flagString(cmd, flagCreateCoreVersionName); explicitVer != "" {
		coreVer = explicitVer
	}

	opts := install.InitOptions{
		Minecraft:    mc,
		Distribution: distribution,
		CoreVersion:  coreVer,
		Repository:   flagString(cmd, flagCreateRepositoryName),
		MCDRVersion:  flagString(cmd, flagCreateMCDRVersionName),
		Directory:    flagString(cmd, flagCreateDirectoryName),
		Force:        flagBool(cmd, flagCreateForceName),
		AllowEmpty:   true,
		Detect:       false,
	}

	return install.Initialize(cmd.Context(), workDir, opts)
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
