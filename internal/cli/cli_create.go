package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/message"
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
	Short: message.CreateShort,
	Args:  cobra.NoArgs,
	RunE:  actionCreate,
}

func NewCreateCommand() *cobra.Command {
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
	return createCmd
}

func parseCoreDistributionCreate(raw string) (distribution, version string) {
	if raw == "" {
		return "", ""
	}
	if dist, ver, found := strings.Cut(raw, "@"); found {
		return dist, ver
	}
	return raw, ""
}

func actionCreate(cmd *cobra.Command, _ []string) error {
	workDir, err := resolveWorkDirCreate(cmd)
	if err != nil {
		return err
	}

	mc := flagStringCreate(cmd, flagCreateMcName)
	if mc == "" {
		mc = flagStringCreate(cmd, flagCreateGameVersionName)
	}

	coreRaw := flagStringCreate(cmd, flagCreateCoreName)
	distribution, coreVer := parseCoreDistributionCreate(coreRaw)
	if explicitVer := flagStringCreate(cmd, flagCreateCoreVersionName); explicitVer != "" {
		coreVer = explicitVer
	}

	opts := install.InitOptions{
		Minecraft:       mc,
		Distribution:    distribution,
		CoreVersion:     coreVer,
		Repository:      flagStringCreate(cmd, flagCreateRepositoryName),
		MCDRVersion:     flagStringCreate(cmd, flagCreateMCDRVersionName),
		Directory:       flagStringCreate(cmd, flagCreateDirectoryName),
		Force:           flagBoolCreate(cmd, flagCreateForceName),
		AllowEmpty:      true,
		Detect:          false,
		PromptMinecraft: promptMinecraft(cmd),
	}

	return initializeWorkspace(cmd, workDir, opts)
}

func resolveWorkDirCreate(cmd *cobra.Command) (string, error) {
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

func flagStringCreate(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func flagBoolCreate(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}
