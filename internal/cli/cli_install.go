package cli

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/javainstall"
	"github.com/mclucy/lucy/operation"
	"github.com/spf13/cobra"
)

const (
	flagInstallLockedName       = "locked"
	flagInstallOfflineName      = "offline"
	flagInstallWithOptionalName = "with-optional"
	flagInstallAcceptBrokenName = "accept-broken"
	flagInstallOverwriteName    = "overwrite"
	flagInstallForceName        = "force"
	flagInstallJavaName         = "java"
	flagInstallJavaRepoName     = "add-java-repository"
	flagInstallMCDRVenvName     = "mcdr-venv"
)

var installCmd = &cobra.Command{
	Use:     "install",
	Short:   help.InstallShort,
	Args:    cobra.NoArgs,
	Aliases: []string{"i"},
	RunE:    actionInstall,
}

func NewInstallCommand() *cobra.Command {
	installCmd.Flags().Bool(
		flagInstallLockedName,
		false,
		"Do not resolve new versions; fail when the lock cannot satisfy",
	)
	installCmd.Flags().Bool(
		flagInstallOfflineName,
		false,
		"Disallow network acquisition; fail if any file is missing or mismatched",
	)
	installCmd.Flags().Bool(
		flagInstallWithOptionalName,
		false,
		"Also install optional dependencies and record them as explicit packages",
	)
	installCmd.Flags().Bool(
		flagInstallAcceptBrokenName,
		false,
		"Complete the installation even when required modules are missing or versions do not satisfy their constraints",
	)
	installCmd.Flags().Bool(
		flagInstallOverwriteName,
		false,
		"Replace files already present at package destinations",
	)
	installCmd.Flags().BoolP(
		flagInstallForceName,
		"f",
		false,
		"Combine --accept-broken and --overwrite",
	)
	installCmd.Flags().Bool(
		flagInstallJavaName,
		false,
		"Also install the Java runtime the server asks for when no installed runtime meets it",
	)
	installCmd.Flags().Bool(
		flagInstallJavaRepoName,
		false,
		"Let --java add the Java vendor's package repository to the system package manager",
	)
	installCmd.Flags().Bool(
		flagInstallMCDRVenvName,
		false,
		"Create an isolated MCDReforged Python environment instead of using the global MCDReforged executable",
	)
	return installCmd
}

func actionInstall(cmd *cobra.Command, _ []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	locked, _ := cmd.Flags().GetBool(flagInstallLockedName)
	offline, _ := cmd.Flags().GetBool(flagInstallOfflineName)
	withOptional, _ := cmd.Flags().GetBool(flagInstallWithOptionalName)
	acceptBroken, _ := cmd.Flags().GetBool(flagInstallAcceptBrokenName)
	overwrite, _ := cmd.Flags().GetBool(flagInstallOverwriteName)
	force, _ := cmd.Flags().GetBool(flagInstallForceName)
	installJava, _ := cmd.Flags().GetBool(flagInstallJavaName)
	addJavaRepository, _ := cmd.Flags().GetBool(flagInstallJavaRepoName)
	mcdrVenv, _ := cmd.Flags().GetBool(flagInstallMCDRVenvName)
	if addJavaRepository && !installJava {
		return fmt.Errorf("--%s needs --%s", flagInstallJavaRepoName, flagInstallJavaName)
	}

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		AcceptBroken:    acceptBroken || force,
		Overwrite:       overwrite || force,
		WithOptional:    withOptional,
		MCDRVenv:        mcdrVenv,
		PublishManifest: false,
	}

	output := newInstallOutput(cmd)
	defer output.Close()
	opts.Trace = output.Trace()
	if installJava {
		opts.ProvisionJava = output.provisionJava(javainstall.Host{
			Interactive:     NewPromptGate(cmd).Available(),
			AllowRepository: addJavaRepository,
		}, offline)
	}
	ctx, stop := CommandContext(cmd)
	defer stop()
	result, err := operation.Sync(ctx, workDir, opts)
	if err != nil {
		return output.Failure(err)
	}
	return output.Render(result)
}
