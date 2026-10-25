package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/javainstall"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
	"github.com/spf13/cobra"
)

const (
	flagCreateForceName       = "force"
	flagCreateCoreName        = "core"
	flagCreateCoreVersionName = "core-version"
	flagCreateMcName          = "minecraft"
	flagCreateMCDRVersionName = "mcdr-version"
	flagCreateDirectoryName   = "directory"
	flagCreateInstallName     = "install"
	flagCreateNoInstallName   = "no-install"
	flagCreateJavaName        = "java"
	flagCreateJavaRepoName    = "add-java-repository"
	createProductMinecraft    = "minecraft"
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: help.CreateShort,
	Long:  help.CreateLong,
	Example: "  lucy create\n" +
		"  lucy create --minecraft 1.21.4 --core paper\n" +
		"  lucy create --directory server --minecraft 1.21.4 --core paper --no-install",
	Args: cobra.NoArgs,
	RunE: actionCreate,
}

func NewCreateCommand() *cobra.Command {
	createCmd.Flags().BoolP(
		flagCreateForceName,
		"f",
		false,
		"Replace an existing manifest in the target directory",
	)
	createCmd.Flags().StringP(
		flagCreateCoreName,
		"c",
		"",
		"Server software, optionally with a version (e.g. paper@232, fabric@0.16.9)",
	)
	createCmd.Flags().String(
		flagCreateCoreVersionName,
		"",
		"Server software version or version rule",
	)
	createCmd.Flags().StringP(
		flagCreateMcName,
		"m",
		"",
		"Target Minecraft version (e.g. 1.21.4)",
	)
	createCmd.Flags().String(
		flagCreateMCDRVersionName,
		"",
		"Record this MCDReforged version",
	)
	createCmd.Flags().StringP(
		flagCreateDirectoryName,
		"d",
		".",
		"Directory to create the server in",
	)
	createCmd.Flags().Bool(
		flagCreateInstallName,
		false,
		"Install the server after writing the manifest and lock (default)",
	)
	createCmd.Flags().Bool(
		flagCreateNoInstallName,
		false,
		"Only write lucy.yaml and lucy-lock.yaml; install later",
	)
	createCmd.Flags().Bool(
		flagCreateJavaName,
		false,
		"Also install the Java runtime the server asks for when no installed runtime meets it",
	)
	createCmd.Flags().Bool(
		flagCreateJavaRepoName,
		false,
		"Let --java add the Java vendor's package repository to the system package manager",
	)
	return createCmd
}

// createChoices carries the values the command has settled before the form or
// the operation. Product is the selector value.
type createChoices struct {
	Directory         string
	Product           string
	Minecraft         string
	CoreVersion       string
	MCDRVersion       string
	Force             bool
	Install           bool
	InstallChosen     bool
	Java              bool
	AddJavaRepository bool
}

func actionCreate(cmd *cobra.Command, _ []string) error {
	ctx, stop := CommandContext(cmd)
	defer stop()

	choices, err := choicesFromCreateFlags(cmd)
	if err != nil {
		return err
	}
	interactive := NewPromptGate(cmd).Available()
	if interactive {
		choices, err = promptCreate(ctx, cmd, choices)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return &report.Detail{
					Title: "creation cancelled",
					Lines: []string{"Nothing was written."},
					Cause: err,
				}
			}
			return err
		}
		if err := validateCreateChoices(choices); err != nil {
			return err
		}
	} else if err := validateCreateChoices(choices); err != nil {
		return err
	}
	if !interactive {
		if err := warnCreateTarget(choices); err != nil {
			return err
		}
	}
	opts := createOperationOptions(choices)

	output := newCreateOutput(cmd)
	defer output.Close()
	opts.Trace = output.Trace()
	if choices.Java && choices.Install {
		opts.InstallOptions.ProvisionJava = output.provisionJava(
			javainstall.Host{
				Interactive:     NewPromptGate(cmd).Available(),
				AllowRepository: choices.AddJavaRepository,
			},
			false,
		)
	}
	result, err := operation.Create(ctx, opts)
	if err != nil {
		return output.Failure(err)
	}
	return output.Render(result)
}

func choicesFromCreateFlags(cmd *cobra.Command) (createChoices, error) {
	installFlag, _ := cmd.Flags().GetBool(flagCreateInstallName)
	noInstallFlag, _ := cmd.Flags().GetBool(flagCreateNoInstallName)
	installSet := cmd.Flags().Changed(flagCreateInstallName)
	noInstallSet := cmd.Flags().Changed(flagCreateNoInstallName)
	installChosen := installSet || noInstallSet
	if installSet && noInstallSet && installFlag == noInstallFlag {
		return createChoices{}, errors.New("--install and --no-install contradict each other")
	}
	install := true
	if cmd.Flags().Changed(flagCreateInstallName) {
		install = installFlag
	}
	if cmd.Flags().Changed(flagCreateNoInstallName) {
		install = !noInstallFlag
	}
	javaFlag, _ := cmd.Flags().GetBool(flagCreateJavaName)
	javaRepository, _ := cmd.Flags().GetBool(flagCreateJavaRepoName)
	if javaRepository && !javaFlag {
		return createChoices{}, fmt.Errorf("--%s needs --%s", flagCreateJavaRepoName, flagCreateJavaName)
	}
	coreRaw := flagStringCreate(cmd, flagCreateCoreName)
	productName, inlineVersion := parseCoreDistributionCreate(coreRaw)
	if explicitVersion := flagStringCreate(cmd, flagCreateCoreVersionName); explicitVersion != "" {
		if inlineVersion != "" && inlineVersion != explicitVersion {
			return createChoices{}, fmt.Errorf(
				"--core includes @%s but --core-version is %s; use one version",
				inlineVersion,
				explicitVersion,
			)
		}
		inlineVersion = explicitVersion
	}
	choices := createChoices{
		Directory:         flagStringCreate(cmd, flagCreateDirectoryName),
		Minecraft:         strings.TrimSpace(flagStringCreate(cmd, flagCreateMcName)),
		CoreVersion:       strings.TrimSpace(inlineVersion),
		MCDRVersion:       strings.TrimSpace(flagStringCreate(cmd, flagCreateMCDRVersionName)),
		Force:             flagBoolCreate(cmd, flagCreateForceName),
		Install:           install,
		InstallChosen:     installChosen,
		Java:              javaFlag,
		AddJavaRepository: javaRepository,
	}
	productName = strings.TrimSpace(productName)
	if productName == "" {
		if choices.CoreVersion != "" {
			return createChoices{}, fmt.Errorf("--%s needs --%s", flagCreateCoreVersionName, flagCreateCoreName)
		}
		choices.Product = createProductMinecraft
		return choices, nil
	}
	product, known := serverpkg.ParseProduct(productName)
	if !known || !createProductSupported(product) {
		return createChoices{}, fmt.Errorf(
			"server software %q is not supported by create; choose minecraft, paper, purpur, fabric, forge, or neoforge",
			productName,
		)
	}
	if product == serverpkg.ProductMinecraft {
		if choices.CoreVersion != "" && choices.CoreVersion != choices.Minecraft {
			return createChoices{}, fmt.Errorf(
				"--%s does not apply to the Minecraft core; the Minecraft version is the server version",
				flagCreateCoreVersionName,
			)
		}
		choices.CoreVersion = ""
	}
	choices.Product = string(product)
	return choices, nil
}

func validateCreateChoices(choices createChoices) error {
	if strings.TrimSpace(choices.Directory) == "" {
		return errors.New("choose a directory with --directory")
	}
	if err := validateCreateMinecraft(choices.Minecraft); err != nil {
		return fmt.Errorf("--%s: %w", flagCreateMcName, err)
	}
	if choices.Java && !choices.Install {
		return errors.New("--java needs installation; omit --no-install or choose Install")
	}
	return nil
}

func createOperationOptions(choices createChoices) operation.CreateOptions {
	product := choices.Product
	if product == "" {
		product = createProductMinecraft
	}
	return operation.CreateOptions{
		Directory:      choices.Directory,
		Minecraft:      choices.Minecraft,
		Product:        product,
		ProductVersion: choices.CoreVersion,
		MCDRVersion:    choices.MCDRVersion,
		Force:          choices.Force,
		Install:        choices.Install,
	}
}

func warnCreateTarget(choices createChoices) error {
	target, err := operation.InspectCreateTarget(choices.Directory)
	if err != nil {
		return err
	}
	if target.ManifestExists && !choices.Force {
		return fmt.Errorf(
			"lucy.yaml exists in %s; pass --force to replace declared intent",
			target.Path,
		)
	}
	if !target.NonEmpty {
		return nil
	}
	location := displayCreatePath(target.Path)
	message := "the current directory is not empty"
	if location != "." {
		message = "directory " + location + " is not empty"
	}
	if len(target.Entries) > 0 {
		message += " (" + strings.Join(target.Entries, ", ") + ")"
	}
	message += "; existing files are left in place"
	if target.ManifestExists {
		message += ", and lucy.yaml will be replaced"
		if target.DeclaredPackages > 0 {
			message += " (" + createPackageCountLabel(target.DeclaredPackages) +
				" declared; installation may remove their files)"
		}
	}
	report.Warn(errors.New(message))
	return nil
}

func parseCoreDistributionCreate(raw string) (distribution, coreVersion string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if dist, ver, found := strings.Cut(raw, "@"); found {
		return dist, ver
	}
	return raw, ""
}

func flagStringCreate(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func flagBoolCreate(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}

func createProductSupported(product serverpkg.Product) bool {
	switch product {
	case serverpkg.ProductMinecraft,
		serverpkg.ProductPaper,
		serverpkg.ProductPurpur,
		serverpkg.ProductFabric,
		serverpkg.ProductForge,
		serverpkg.ProductNeoForge:
		return true
	default:
		return false
	}
}

func validateCreateMinecraft(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("enter a Minecraft version")
	}
	parsed := version.Version(value)
	if parsed.CanInfer() || parsed.IsInvalid() {
		return errors.New("enter an exact Minecraft version, e.g. 1.21.4")
	}
	switch strings.ToLower(value) {
	case "latest", "snapshot":
		return errors.New("enter an exact Minecraft version, e.g. 1.21.4")
	}
	return nil
}

// displayCreatePath keeps the form and the final summary short when the
// target is below the current directory.
func displayCreatePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return absolute
	}
	relative, err := filepath.Rel(cwd, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return absolute
	}
	return relative
}

// displayCreateLocation names the current directory in prose instead of
// leaving a bare dot in a sentence.
func displayCreateLocation(path string) string {
	display := displayCreatePath(path)
	if display == "." {
		return "the current directory"
	}
	return display
}
