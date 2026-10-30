package cli

import (
	"fmt"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/javainstall"
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

func NewInstallCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "install",
		Short:   help.InstallShort,
		Args:    cobra.NoArgs,
		Aliases: []string{"i"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionInstall(cmd, inv, deps, args)
		},
	}
	installJSON.Bind(contracts, cmd)
	cmd.Flags().Bool(
		flagInstallLockedName,
		false,
		"Do not resolve new versions; fail when the lock cannot satisfy",
	)
	cmd.Flags().Bool(
		flagInstallOfflineName,
		false,
		"Disallow network acquisition; fail if any file is missing or mismatched",
	)
	cmd.Flags().Bool(
		flagInstallWithOptionalName,
		false,
		"Also install optional dependencies and record them as explicit packages",
	)
	cmd.Flags().Bool(
		flagInstallAcceptBrokenName,
		false,
		"Complete the installation even when required modules are missing or versions do not satisfy their constraints",
	)
	cmd.Flags().Bool(
		flagInstallOverwriteName,
		false,
		"Replace files already present at package destinations",
	)
	cmd.Flags().BoolP(
		flagInstallForceName,
		"f",
		false,
		"Combine --accept-broken and --overwrite",
	)
	cmd.Flags().Bool(
		flagInstallJavaName,
		false,
		"Also install the Java runtime the server asks for when no installed runtime meets it",
	)
	cmd.Flags().Bool(
		flagInstallJavaRepoName,
		false,
		"Let --java add the Java vendor's package repository to the system package manager",
	)
	cmd.Flags().Bool(
		flagInstallMCDRVenvName,
		false,
		"Create an isolated MCDReforged Python environment instead of using the global MCDReforged executable",
	)
	return cmd
}

func actionInstall(cmd *cobra.Command, inv *Invocation, deps *Dependencies, _ []string) error {
	target, err := ResolveCommandTarget(cmd, inv)
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	if target.Registered {
		if addRepo, _ := cmd.Flags().GetBool(flagInstallJavaRepoName); addRepo {
			return fmt.Errorf("--%s requires a direct administrative invocation, not a daemon package task", flagInstallJavaRepoName)
		}
		return DispatchPackageTask(cmd, inv, target, PackageTaskRequest{Name: TaskInstall})
	}
	return RunInstallTask(cmd, inv, deps)
}

// RunInstallTask retains all install options and output contracts in daemon children.
func RunInstallTask(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
) error {
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

	output := newInstallOutput(cmd, inv)
	defer output.Close()
	opts.Trace = output.Trace()
	if installJava {
		opts.ProvisionJava = output.provisionJava(
			deps.JavaHost,
			javainstall.Policy{
				Interactive:     NewPromptGate(cmd, inv).Available(),
				AllowRepository: addJavaRepository,
			},
			offline,
		)
	}
	ctx, stop := CommandContext(cmd)
	defer stop()
	result, err := deps.Operations.Sync(ctx, inv.Directory, opts)
	if err != nil {
		return output.Failure(err)
	}
	return output.Render(result)
}
