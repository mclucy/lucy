package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"unicode"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/operation"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream/providers/mojang"
	"github.com/spf13/cobra"
)

// promptCreate collects the choices for a new workspace. Flags prefill the
// form; the form remains the authority for an interactive invocation.
func promptCreate(
	ctx context.Context,
	cmd *cobra.Command,
	defaults createChoices,
) (createChoices, error) {
	values := defaults
	forceFromFlag := values.Force
	if values.Directory == "" {
		values.Directory = "."
	}
	if values.Product == "" {
		values.Product = createProductMinecraft
	}
	if values.CoreName == "" {
		values.CoreName = customCoreName(values.Repository)
	}
	directory := huh.NewInput().
		Key("directory").
		Title("Directory").
		DescriptionFunc(func() string {
			return describeCreateDirectory(values.Directory, values.Force)
		}, &values.Directory).
		Placeholder(".").
		Value(&values.Directory).
		Validate(func(path string) error {
			return validateCreateDirectory(path)
		})
	product := huh.NewSelect[string]().
		Key("product").
		Title("Server software").
		Description("Choose the server implementation to create.").
		Options(createProductOptions()...).
		Value(&values.Product)
	var minecraftVersions atomic.Value
	go func() {
		manifest, err := mojang.FetchVersionManifest()
		if err != nil {
			return
		}
		versions := make([]string, 0, 100)
		for _, entry := range manifest.Versions {
			if entry.Type != "release" {
				continue
			}
			versions = append(versions, entry.ID)
			if len(versions) == 100 {
				break
			}
		}
		minecraftVersions.Store(versions)
	}()
	minecraft := huh.NewInput().
		Key("minecraft").
		Title("Minecraft version").
		Placeholder("1.21.4").
		Description("An exact release or snapshot ID, for example 1.21.4. Start typing for known versions.").
		Value(&values.Minecraft).
		SuggestionsFunc(func() []string {
			versions, _ := minecraftVersions.Load().([]string)
			return versions
		}, &values.Minecraft).
		Validate(func(value string) error {
			return validateCreateMinecraft(value)
		})
	coreVersion := huh.NewInput().
		Key("core-version").
		Title("Server software version").
		PlaceholderFunc(func() string {
			return createCoreVersionPlaceholder(values.Product)
		}, &values.Product).
		DescriptionFunc(func() string {
			return createCoreVersionDescription(values.Product)
		}, &values.Product).
		Value(&values.CoreVersion)
	coreName := huh.NewInput().
		Key("core-name").
		Title("Server name").
		Description("The name Lucy records for this server, for example my-fork.").
		Placeholder("my-fork").
		Value(&values.CoreName).
		Validate(func(value string) error {
			return validateCreateCoreName(value)
		})
	repository := huh.NewInput().
		Key("repository").
		Title("GitHub repository").
		Description("Owner and repository that publish a Paperclip server JAR in a release.").
		Placeholder("owner/repo").
		Value(&values.Repository).
		Validate(func(value string) error {
			return validateCreateRepository(value)
		})
	filePattern := huh.NewInput().
		Key("file-pattern").
		Title("Release asset pattern").
		Description("Optional glob for the server JAR, for example server-*.jar.").
		Placeholder("server-*.jar").
		Value(&values.FilePattern).
		Validate(func(value string) error {
			return validateCreateFilePattern(value)
		})

	groups := []*huh.Group{
		huh.NewGroup(directory, product, minecraft).
			Title("New server").
			Description("Choose where the server lives and what it runs."),
		huh.NewGroup(coreVersion).
			Title("Server version").
			Description("Pick an exact build or let Lucy choose the latest.").
			WithHideFunc(func() bool {
				return values.Product == createProductMinecraft
			}),
		huh.NewGroup(coreName, repository, filePattern).
			Title("GitHub-hosted server").
			Description("Lucy downloads the server JAR from a GitHub release. It must be a Paperclip-based fork.").
			WithHideFunc(func() bool {
				return values.Product != createProductCustom
			}),
	}
	summary := huh.NewNote().
		Title("Review").
		DescriptionFunc(func() string {
			return createFormSummary(values)
		}, &values)
	if values.InstallChosen {
		summary = summary.Next(true).NextLabel("Create")
		groups = append(groups, huh.NewGroup(summary).
			Title("Finish").
			Description("Review the choices, then create the server."))
	} else {
		install := huh.NewConfirm().
			Key("install").
			Title("Install the server now?").
			Description("Not yet writes only lucy.yaml and lucy-lock.yaml; run lucy install later.").
			Affirmative("Install").
			Negative("Not yet").
			Value(&values.Install)
		if values.Java {
			install = install.Validate(func(install bool) error {
				if !install {
					return errors.New("--java needs installation; choose Install")
				}
				return nil
			})
		}
		groups = append(groups, huh.NewGroup(summary, install).
			Title("Finish").
			Description("Confirm the installation choice to create the server."))
	}
	replaceGroup := huh.NewGroup(
		huh.NewConfirm().
			Key("replace").
			Title("Replace existing manifest?").
			DescriptionFunc(func() string {
				target, err := operation.InspectCreateTarget(values.Directory)
				if err != nil || !target.ManifestExists {
					return ""
				}
				if target.DeclaredPackages == 0 {
					return "The existing lucy.yaml will be replaced."
				}
				return "The existing lucy.yaml declares " +
					createPackageCountLabel(target.DeclaredPackages) +
					". Installation may remove files those packages own."
			}, &values.Directory).
			Affirmative("Replace").
			Negative("Choose another directory").
			Value(&values.Force).
			Validate(func(replace bool) error {
				if !replace {
					return errors.New("choose another directory or confirm replacement")
				}
				return nil
			}),
	).
		Title("Existing manifest").
		Description("Confirm before replacing the existing lucy.yaml.").
		WithHideFunc(func() bool {
			if forceFromFlag {
				return true
			}
			target, err := operation.InspectCreateTarget(values.Directory)
			return err != nil || !target.ManifestExists
		})
	groups = append([]*huh.Group{groups[0], replaceGroup}, groups[1:]...)
	form := huh.NewForm(groups...).
		WithInput(cmd.InOrStdin()).
		WithOutput(cmd.ErrOrStderr())
	if err := NormalizePromptError(ctx, form.RunWithContext(ctx)); err != nil {
		return createChoices{}, err
	}
	values.Directory = strings.TrimSpace(values.Directory)
	if values.Directory == "" {
		values.Directory = "."
	}
	values.Minecraft = strings.TrimSpace(values.Minecraft)
	values.CoreVersion = strings.TrimSpace(values.CoreVersion)
	values.Repository = strings.TrimSpace(values.Repository)
	values.FilePattern = strings.TrimSpace(values.FilePattern)
	if values.Product == createProductCustom {
		values.CoreName = strings.ToLower(strings.TrimSpace(values.CoreName))
		if values.CoreName == "" {
			values.CoreName = customCoreName(values.Repository)
		}
	} else {
		values.CoreName = ""
		values.Repository = ""
		values.FilePattern = ""
		if values.Product == createProductMinecraft {
			values.CoreVersion = ""
		}
	}
	return values, nil
}

func createProductOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("Minecraft (vanilla)", createProductMinecraft),
		huh.NewOption("Paper", string(serverpkg.ProductPaper)),
		huh.NewOption("Purpur", string(serverpkg.ProductPurpur)),
		huh.NewOption("Fabric", string(serverpkg.ProductFabric)),
		huh.NewOption("Forge", string(serverpkg.ProductForge)),
		huh.NewOption("NeoForge", string(serverpkg.ProductNeoForge)),
		huh.NewOption("Custom Paper fork (GitHub)", createProductCustom),
	}
}

func createProductTitle(product string) string {
	if product == createProductCustom {
		return "Custom"
	}
	if parsed, known := serverpkg.ParseProduct(product); known {
		return parsed.Title()
	}
	if product == "" {
		return "Minecraft"
	}
	runes := []rune(product)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func createCoreVersionDescription(product string) string {
	switch product {
	case string(serverpkg.ProductPaper):
		return "Paper build number such as 232, or stable / any."
	case string(serverpkg.ProductPurpur):
		return "Purpur build number, latest, or any."
	case string(serverpkg.ProductFabric):
		return "Fabric loader version such as 0.16.9, or any for the latest."
	case string(serverpkg.ProductForge):
		return "Forge version such as 47.3.0, or any for the latest."
	case string(serverpkg.ProductNeoForge):
		return "NeoForge version such as 21.1.233, or any for the latest."
	case createProductCustom:
		return "Release tag, or any for the latest release."
	default:
		return "Leave empty to let Lucy choose the latest version."
	}
}

func createCoreVersionPlaceholder(product string) string {
	switch product {
	case string(serverpkg.ProductPaper):
		return "232"
	case string(serverpkg.ProductPurpur):
		return "latest"
	case string(serverpkg.ProductFabric):
		return "0.16.9"
	case string(serverpkg.ProductForge):
		return "47.3.0"
	case string(serverpkg.ProductNeoForge):
		return "21.1.233"
	case createProductCustom:
		return "v1.0.0"
	default:
		return "any"
	}
}

func createFormSummary(values createChoices) string {
	serverVersion := strings.TrimSpace(values.CoreVersion)
	if values.Product == createProductMinecraft {
		serverVersion = "the Minecraft version"
	} else if serverVersion == "" {
		serverVersion = "any (latest)"
	}
	server := createProductTitle(values.Product)
	if values.Product == createProductCustom {
		coreName := strings.TrimSpace(values.CoreName)
		if coreName == "" {
			coreName = customCoreName(values.Repository)
		}
		server = coreName + " (GitHub)"
	}
	lines := []string{
		"Directory: " + displayCreateLocation(values.Directory),
		"Server: " + server + " for Minecraft " + values.Minecraft,
		"Server version: " + serverVersion,
	}
	if values.Product == createProductCustom {
		lines = append(lines, "Repository: "+values.Repository)
		if values.FilePattern != "" {
			lines = append(lines, "Asset pattern: "+values.FilePattern)
		}
	}
	install := "not yet"
	if values.Install {
		install = "yes"
	}
	lines = append(lines, "Install after creation: "+install)
	if target, err := operation.InspectCreateTarget(values.Directory); err == nil {
		if target.NonEmpty {
			lines = append(lines, "Warning: the directory is not empty.")
		}
		if target.ManifestExists && values.Force {
			replacement := "Existing lucy.yaml will be replaced."
			if target.DeclaredPackages > 0 {
				replacement = "Existing lucy.yaml will be replaced; it declares " +
					createPackageCountLabel(target.DeclaredPackages) + "."
			}
			lines = append(lines, replacement)
		}
	}
	return strings.Join(lines, "\n")
}

func describeCreateDirectory(path string, force bool) string {
	usingCurrent := strings.TrimSpace(path) == ""
	if usingCurrent {
		path = "."
	}
	target, err := operation.InspectCreateTarget(path)
	if err != nil {
		return err.Error()
	}
	prefix := ""
	if usingCurrent {
		prefix = "Using the current directory. "
	}
	if !target.Exists {
		return prefix + "This directory will be created."
	}
	if !target.NonEmpty {
		return prefix + "This directory is empty."
	}
	description := prefix + "Warning: this directory is not empty"
	if len(target.Entries) > 0 {
		description += " (" + strings.Join(target.Entries, ", ")
		if len(target.Entries) == 5 {
			description += ", …"
		}
		description += ")"
	}
	description += ". Existing files stay in place; installation may conflict."
	if target.ManifestExists {
		if force {
			description += " An existing lucy.yaml will be replaced."
			if target.DeclaredPackages > 0 {
				description += " It declares " + createPackageCountLabel(target.DeclaredPackages) +
					"; installation may remove files those packages own."
			}
		} else {
			description += " lucy.yaml already exists; confirm replacement on the next step."
		}
	}
	return description
}

func createPackageCountLabel(count int) string {
	if count == 1 {
		return "1 package"
	}
	return fmt.Sprintf("%d packages", count)
}

func validateCreateDirectory(path string) error {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	_, err := operation.InspectCreateTarget(path)
	return err
}

func validateCreateRepository(value string) error {
	value = strings.TrimSpace(value)
	owner, name, found := strings.Cut(value, "/")
	if !found || owner == "" || name == "" || strings.Contains(name, "/") {
		return errors.New("use owner/repo")
	}
	return nil
}

func validateCreateCoreName(value string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, " /\\") {
		return errors.New("use one word, for example my-fork")
	}
	return nil
}

func validateCreateFilePattern(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if _, err := filepath.Match(value, "server.jar"); err != nil {
		return errors.New("enter a valid glob, for example server-*.jar")
	}
	return nil
}
