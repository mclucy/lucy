package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/internal/shell"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

// reportObservationFindings renders each consumed observation once.
func reportObservationFindings(observed *workspace.Workspace, output *Output) {
	if observed == nil {
		return
	}
	for _, warning := range observed.Warnings {
		output.Warn(warning)
	}
	reportDiscoveryFindings(&observed.Discovery, output)
}

// reportDiscoveryFindings presents one discovery result. A resolved layout
// never reports a failure: automatic choices are warnings, and unmanaged or
// alternative entry points stay informational.
func reportDiscoveryFindings(observed *discovery.Result, output *Output) {
	if observed == nil {
		return
	}
	selection := observed.Selection
	resolved := selection.IsValid()
	automatic := selection.Source == discovery.SourcePreferred
	unresolvedReported := false
	unreadableReported := map[string]bool{}
	for finding := range observed.Findings() {
		switch finding.Kind {
		case discovery.FindingAutomaticSelection:
			output.Warn(automaticSelectionMessage(finding))
		case discovery.FindingUnreadableFile:
			key := unreadableFindingKey(finding)
			if unreadableReported[key] {
				continue
			}
			unreadableReported[key] = true
			output.Warn(unreadableFindingError(finding))
		case discovery.FindingUnmanagedInstallation:
			message := installationFindingMessage(finding)
			if output.JSON() {
				output.Warn(errors.New(message))
			} else {
				output.Info(message)
			}
		case discovery.FindingMissingDeclaredInstallation:
			output.Warn(errors.New(
				"declared server installation is missing; run lucy install --locked",
			))
		case discovery.FindingOwnershipError:
			output.Warn(fmt.Errorf(
				"cannot verify ownership of %s: %w",
				finding.Path,
				finding.Cause,
			))
		case discovery.FindingModifiedFile:
			output.Warn(fmt.Errorf(
				"managed file %s was modified",
				finding.Path,
			))
		case discovery.FindingReceiptError:
			if finding.Cause != nil {
				output.Warn(fmt.Errorf(
					"cannot verify installation ownership: %w",
					finding.Cause,
				))
			} else {
				output.Warn(errors.New("cannot verify installation ownership"))
			}
		case discovery.FindingInvalidIdentity:
			output.Warn(fmt.Errorf(
				"cannot identify the server in %s: %v",
				finding.Path,
				finding.Cause,
			))
		case discovery.FindingConflictingIdentities,
			discovery.FindingAmbiguousInstallations:
			if resolved || automatic || unresolvedReported {
				continue
			}
			message := unresolvedRuntimeMessage(finding)
			if output.JSON() {
				output.Warn(message)
			} else {
				output.Error(message)
			}
			unresolvedReported = true
		}
	}
}

// unreadableFindingKey identifies one underlying read failure. Recognizers
// can inspect the same artifact, so identical path and cause pairs collapse.
func unreadableFindingKey(finding discovery.Finding) string {
	cause := ""
	if finding.Cause != nil {
		cause = finding.Cause.Error()
	}
	return finding.Path + "\x00" + cause
}

func unreadableFindingError(finding discovery.Finding) error {
	if finding.Cause != nil {
		return fmt.Errorf("cannot read %s: %w", finding.Path, finding.Cause)
	}
	return fmt.Errorf("cannot read %s", finding.Path)
}

// unreadableFindingLines presents each underlying read failure once for a
// command failure detail.
func unreadableFindingLines(result *discovery.Result) []string {
	if result == nil {
		return nil
	}
	var lines []string
	seen := map[string]bool{}
	for _, finding := range result.Diagnostics {
		if finding.Kind != discovery.FindingUnreadableFile {
			continue
		}
		key := unreadableFindingKey(finding)
		if seen[key] {
			continue
		}
		seen[key] = true
		if finding.Cause != nil {
			lines = append(
				lines,
				fmt.Sprintf("%s: %v", finding.Path, finding.Cause),
			)
		} else {
			lines = append(lines, finding.Path)
		}
	}
	return lines
}

func installationFindingMessage(finding discovery.Finding) string {
	label := "unmanaged server"
	switch finding.Ownership {
	case discovery.OwnershipModified:
		label = "modified server"
	case discovery.OwnershipUnreadable, discovery.OwnershipUnknown:
		label = "unreadable server"
	}
	return fmt.Sprintf(
		"%s %s; left unchanged",
		label,
		installationIdentityLabel(finding.Identity),
	)
}

// automaticSelectionMessage names the chosen installation and every competing
// installation. The choice is read-only; the message suggests an explicit
// selection command.
func automaticSelectionMessage(finding discovery.Finding) error {
	selected := installationIdentityLabel(finding.Identity)
	message := fmt.Sprintf(
		"multiple server runtimes found; using %s automatically",
		selected,
	)
	if len(finding.Identities) > 0 {
		competitors := make([]string, 0, len(finding.Identities))
		for _, identity := range finding.Identities {
			competitors = append(competitors, installationIdentityLabel(identity))
		}
		message += "; also present: " + strings.Join(competitors, ", ")
	}
	if command := identitySelectionCommand(finding.Identity); command != "" {
		message += "; run " + command + " to select one"
	}
	return errors.New(message)
}

func unresolvedRuntimeMessage(finding discovery.Finding) error {
	identities := finding.Identities
	if len(identities) == 0 && finding.Identity.IsValid() {
		identities = []discovery.Identity{finding.Identity}
	}
	if len(identities) == 0 {
		return errors.New("no unambiguous server runtime was found")
	}
	commands := make([]string, 0, len(identities))
	seen := map[string]bool{}
	for _, identity := range identities {
		command := identitySelectionCommand(identity)
		if command == "" || seen[command] {
			continue
		}
		seen[command] = true
		commands = append(commands, command)
	}
	if len(commands) == 0 {
		return errors.New("no unambiguous server runtime was found")
	}
	return fmt.Errorf(
		"cannot choose between server runtimes; select one with %s",
		strings.Join(commands, " or "),
	)
}

// installationIdentityLabel renders one installation identity for prompts and
// diagnostics.
func installationIdentityLabel(identity discovery.Identity) string {
	name := identity.Primary.Name.String()
	if product := identity.Product(); product != serverpkg.ProductUnknown {
		name = product.Title()
	}
	parts := make([]string, 0, 2)
	if name = strings.TrimSpace(name); name != "" {
		parts = append(parts, name)
	}
	if version := identity.Primary.Version.String(); strings.TrimSpace(version) != "" {
		parts = append(parts, version)
	}
	label := strings.Join(parts, " ")
	if minecraft := identity.MinecraftVersion(); concreteIdentityVersion(minecraft) &&
		identity.Product() != serverpkg.ProductMinecraft {
		label += " / Minecraft " + minecraft.String()
	}
	if label == "" {
		return "unknown server"
	}
	return label
}

// identitySelectionCommand renders a non-interactive selection for one
// installation identity.
func identitySelectionCommand(identity discovery.Identity) string {
	parts := []string{"lucy", "init"}
	switch product := identity.Product(); {
	case product != serverpkg.ProductUnknown:
		parts = append(parts, "--core", shell.Quote(product.String()))
	case identity.Primary.Name.String() != "":
		parts = append(parts, "--core", shell.Quote(identity.Primary.Name.String()))
	default:
		return ""
	}
	if version := identity.Primary.Version; concreteIdentityVersion(version) {
		parts = append(parts, "--core-version", shell.Quote(version.String()))
	}
	if minecraft := identity.MinecraftVersion(); concreteIdentityVersion(minecraft) {
		parts = append(parts, "--minecraft", shell.Quote(minecraft.String()))
	}
	return strings.Join(parts, " ")
}

func concreteIdentityVersion(value versionpkg.Version) bool {
	return value != "" && !value.IsInvalid() && !value.CanInfer()
}
