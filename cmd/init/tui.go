package init

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mclucy/lucy/state"
)

// RunInteractiveInit walks the user through the interactive init flow via huh
// forms, populating s in-place. Sets s.Aborted=true on cancellation at any
// step, s.Confirmed=true on final approval. No file I/O occurs here.
func RunInteractiveInit(s *InitFlowState) error {
	var continueInit bool
	welcomeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Welcome to Lucy").
				Description(
					"lucy init sets up a new Lucy-managed Minecraft server environment in the\n"+
						"current directory. It will create the following files:\n\n"+
						"  .lucy/config.toml   – policy and source defaults\n"+
						"  .lucy/manifest.toml – environment intent (game version, runtime, compatible platforms, mods)\n"+
						"  .lucy/lock.json     – resolved dependency graph\n\n"+
						"No files will be written until you confirm at the final review step.",
				),
			huh.NewConfirm().
				Title("Continue with setup?").
				Affirmative("Yes, let's go").
				Negative("Cancel").
				Value(&continueInit),
		),
	)
	if err := welcomeForm.Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("welcome step: %w", err)
	}
	if !continueInit {
		s.Aborted = true
		return nil
	}
	s.CurrentStep = StepGameVersion

	if len(s.ExistingFiles) > 0 {
		conflictDesc := fmt.Sprintf(
			"The following Lucy files already exist in this directory:\n\n  %s\n\n"+
				"How should lucy init handle them?",
			strings.Join(s.ExistingFiles, "\n  "),
		)
		if len(s.ExistingStateConflicts) > 0 {
			conflictDesc += "\n\nConflicts to resolve before writing:\n\n  " + strings.Join(s.ExistingStateConflicts, "\n  ")
		}
		conflictMode := string(s.ConflictResolution)
		conflictForm := huh.NewForm(
			huh.NewGroup(
				huh.NewNote().
					Title("Existing Files Detected").
					Description(conflictDesc),
				huh.NewSelect[string]().
					Title("Conflict resolution").
					Options(
						huh.NewOption("Keep existing files, only scaffold missing ones (recommended)", string(PreserveExisting)),
						huh.NewOption("Abort if any file exists – do nothing", string(AbortOnConflict)),
						huh.NewOption("Overwrite everything – replace all existing files", string(OverwriteAll)),
					).
					Value(&conflictMode),
			),
		)
		if err := conflictForm.Run(); err != nil {
			if isUserAbort(err) {
				s.Aborted = true
				return nil
			}
			return fmt.Errorf("conflict resolution step: %w", err)
		}
		s.ConflictResolution = ConflictMode(conflictMode)
		if s.ConflictResolution == AbortOnConflict {
			s.Aborted = true
			fmt.Printf("\nInit aborted: existing files would be overwritten. Use --conflict=overwrite to replace them.\n")
			return nil
		}
	}

	if len(s.ExistingStateConflicts) > 0 {
		s.Aborted = true
		fmt.Printf("\nInit aborted: existing Lucy state has conflicts that must be resolved first.\n  %s\n", strings.Join(s.ExistingStateConflicts, "\n  "))
		return nil
	}

	gameVersionPlaceholder := "1.21.4"
	if s.DiscoveredDefaults.GameVersion != "" {
		gameVersionPlaceholder = s.DiscoveredDefaults.GameVersion
	}

	gameVersionForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Minecraft game version").
				Description("Enter the Minecraft server version this environment targets (e.g. 1.21.4).").
				Placeholder(gameVersionPlaceholder).
				Validate(func(v string) error {
					v = strings.TrimSpace(v)
					if v == "" {
						return errors.New("game version is required")
					}
					return nil
				}).
				Value(&s.GameVersion),
		),
	)
	if err := gameVersionForm.Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("game version step: %w", err)
	}
	s.GameVersion = strings.TrimSpace(s.GameVersion)
	s.CurrentStep = StepPlatform

	platformForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Primary runtime").
				Description("Choose the main server runtime Lucy should treat as the primary host environment.").
				Options(
					huh.NewOption("Fabric – lightweight, fast-updating mod loader", "fabric"),
					huh.NewOption("NeoForge – community fork of Forge (recommended for 1.20.2+)", "neoforge"),
					huh.NewOption("Forge – original mod loader", "forge"),
					huh.NewOption("MCDR – independent controller/plugin framework", "mcdr"),
					huh.NewOption("None / Vanilla – no modding platform", "none"),
				).
				Value(&s.Platform),
		),
	)
	if err := platformForm.Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("platform step: %w", err)
	}
	s.CurrentStep = StepPlatformVersion

	compatibleOptions := state.CompatiblePlatformOptions(s.Platform)
	if len(compatibleOptions) > 0 {
		selected := append([]string(nil), s.CompatiblePlatforms...)
		options := make([]huh.Option[string], 0, len(compatibleOptions))
		for _, platform := range compatibleOptions {
			label := compatiblePlatformLabel(platform)
			options = append(options, huh.NewOption(label, platform))
		}
		compatibleForm := huh.NewForm(
			huh.NewGroup(
				huh.NewMultiSelect[string]().
					Title("Additional compatible platforms").
					Description("Select extra compatibility layers Lucy should record alongside the primary runtime. Only valid combinations for the chosen runtime are shown.").
					Options(options...).
					Value(&selected),
			),
		)
		if err := compatibleForm.Run(); err != nil {
			if isUserAbort(err) {
				s.Aborted = true
				return nil
			}
			return fmt.Errorf("compatible platforms step: %w", err)
		}
		s.CompatiblePlatforms = selected
	} else {
		s.CompatiblePlatforms = nil
	}
	if err := ValidatePlatformSelection(s.Platform, s.CompatiblePlatforms); err != nil {
		return fmt.Errorf("platform selection step: %w", err)
	}

	if s.Platform != "" && s.Platform != "none" {
		platformVersionForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Platform loader version").
					Description(fmt.Sprintf("Enter the %s loader version, or leave blank to use the latest.", s.Platform)).
					Placeholder("latest").
					Value(&s.PlatformVersion),
			),
		)
		if err := platformVersionForm.Run(); err != nil {
			if isUserAbort(err) {
				s.Aborted = true
				return nil
			}
			return fmt.Errorf("platform version step: %w", err)
		}
		s.PlatformVersion = strings.TrimSpace(s.PlatformVersion)
	}
	s.CurrentStep = StepManagedScope

	allRoots := []string{"mods", "plugins", "config", "datapacks", "resourcepacks"}
	managedOpts := make([]huh.Option[string], len(allRoots))
	for i, root := range allRoots {
		managedOpts[i] = huh.NewOption(root, root)
	}
	managedRootsForm := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Managed directories").
				Description("Select which directories Lucy should track and manage.").
				Options(managedOpts...).
				Value(&s.ManagedRoots),
		),
	)
	if err := managedRootsForm.Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("managed scope step: %w", err)
	}

	if len(s.PackageClassifications) > 0 {
		s.CurrentStep = StepPackageClassification
		requiredLeafIDs := make([]string, 0, len(s.PackageClassifications))
		ignoredIDs := make([]string, 0, len(s.PackageClassifications))
		leafOptions := make([]huh.Option[string], 0, len(s.PackageClassifications))
		ignoreOptions := make([]huh.Option[string], 0, len(s.PackageClassifications))
		for _, classification := range s.PackageClassifications {
			label := packageClassificationLabel(classification)
			ignoreOptions = append(ignoreOptions, huh.NewOption(label, classification.ID))
			if classification.Leaf {
				leafOptions = append(leafOptions, huh.NewOption(label, classification.ID))
			}
			if classification.Leaf && classification.Role == state.RoleRequired {
				requiredLeafIDs = append(requiredLeafIDs, classification.ID)
			}
			if classification.Role == state.RoleIgnored {
				ignoredIDs = append(ignoredIDs, classification.ID)
			}
		}

		fields := []huh.Field{
			huh.NewNote().
				Title("Package graph classification").
				Description(buildPackageClassificationDescription(s)),
		}
		if len(leafOptions) > 0 {
			fields = append(fields,
				huh.NewMultiSelect[string]().
					Title("Leaf packages to keep as required").
					Description("Leaf nodes are packages nothing else in the discovered graph depends on. Selected leaves become required; unselected leaves fall back to transitive.").
					Options(leafOptions...).
					Value(&requiredLeafIDs),
			)
		}
		fields = append(fields,
			huh.NewMultiSelect[string]().
				Title("Packages Lucy should ignore").
				Description("Ignored packages remain visible in state, but Lucy will leave them outside managed sync.").
				Options(ignoreOptions...).
				Value(&ignoredIDs),
		)

		classificationForm := huh.NewForm(huh.NewGroup(fields...))
		if err := classificationForm.Run(); err != nil {
			if isUserAbort(err) {
				s.Aborted = true
				return nil
			}
			return fmt.Errorf("package classification step: %w", err)
		}
		applyTakeoverPackageSelections(s, requiredLeafIDs, ignoredIDs)
	}
	s.CurrentStep = StepReview

	summary := buildSummary(s)
	var confirmWrite bool
	reviewForm := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Review – Ready to initialize").
				Description(summary),
			huh.NewConfirm().
				Title("Write these files?").
				Affirmative("Yes, initialize").
				Negative("Cancel").
				Value(&confirmWrite),
		),
	)
	if err := reviewForm.Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("review step: %w", err)
	}

	if !confirmWrite {
		s.Aborted = true
		return nil
	}

	s.Confirmed = true
	s.CurrentStep = StepDone
	return nil
}

func buildSummary(s *InitFlowState) string {
	var sb strings.Builder

	if s.DiscoveredDefaults.Confidence != ConfidenceNone {
		sb.WriteString("Observed server facts\n")
		sb.WriteString("─────────────────────\n")
		obs := s.DiscoveredDefaults
		if obs.GameVersion != "" {
			_, _ = fmt.Fprintf(&sb, "  Game version:  %s\n", obs.GameVersion)
		} else {
			sb.WriteString("  Game version:  (not detected)\n")
		}
		if obs.Platform != "" && obs.Platform != "none" {
			_, _ = fmt.Fprintf(&sb, "  Runtime:       %s", obs.Platform)
			if obs.PlatformVersion != "" {
				_, _ = fmt.Fprintf(&sb, " %s", obs.PlatformVersion)
			}
			sb.WriteString("\n")
		} else {
			sb.WriteString("  Runtime:       (not detected)\n")
		}
		if len(obs.ManagedRoots) > 0 {
			_, _ = fmt.Fprintf(&sb, "  Directories:   %s\n", strings.Join(obs.ManagedRoots, ", "))
		}
		if len(obs.DetectedPackages) > 0 {
			_, _ = fmt.Fprintf(&sb, "  Packages:      %d detected\n", len(obs.DetectedPackages))
		}
		_, _ = fmt.Fprintf(&sb, "  Confidence:    %s\n", obs.Confidence)
		sb.WriteString("\n")
	}

	sb.WriteString("Proposed manifest intent\n")
	sb.WriteString("────────────────────────\n")
	_, _ = fmt.Fprintf(&sb, "  Game version:  %s\n", s.GameVersion)
	if s.Platform == "" || s.Platform == "none" {
		sb.WriteString("  Primary runtime: none (vanilla)\n")
	} else {
		_, _ = fmt.Fprintf(&sb, "  Primary runtime: %s\n", s.Platform)
		if s.PlatformVersion != "" {
			_, _ = fmt.Fprintf(&sb, "  Loader version:  %s\n", s.PlatformVersion)
		} else {
			sb.WriteString("  Loader version:  (latest)\n")
		}
	}
	if len(s.CompatiblePlatforms) > 0 {
		_, _ = fmt.Fprintf(&sb, "  Compatible with: %s\n", strings.Join(s.CompatiblePlatforms, ", "))
	}
	if len(s.ManagedRoots) > 0 {
		_, _ = fmt.Fprintf(&sb, "  Managed dirs:    %s\n", strings.Join(s.ManagedRoots, ", "))
	} else {
		sb.WriteString("  Managed dirs:    (none selected)\n")
	}
	_, _ = fmt.Fprintf(&sb, "  Conflict mode:   %s\n", s.ConflictResolution)
	if len(s.ExistingFiles) > 0 {
		_, _ = fmt.Fprintf(&sb, "  Existing files:  %s (will be %s)\n",
			strings.Join(s.ExistingFiles, ", "),
			conflictModeVerb(s.ConflictResolution),
		)
	}
	sb.WriteString("\n")

	if len(s.PackageClassifications) > 0 {
		sb.WriteString("Package roles\n")
		sb.WriteString("─────────────\n")
		var required, transitive, ignored []string
		for _, classification := range s.PackageClassifications {
			entry := fmt.Sprintf("%s (%s)", classification.ID, packageClassificationKind(classification))
			switch classification.Role {
			case state.RoleRequired:
				required = append(required, entry)
			case state.RoleIgnored:
				ignored = append(ignored, entry)
			default:
				transitive = append(transitive, entry)
			}
		}
		if len(required) > 0 {
			_, _ = fmt.Fprintf(&sb, "  Required:    %s\n", strings.Join(required, ", "))
		}
		if len(transitive) > 0 {
			_, _ = fmt.Fprintf(&sb, "  Transitive:  %s\n", strings.Join(transitive, ", "))
		}
		if len(ignored) > 0 {
			_, _ = fmt.Fprintf(&sb, "  Ignored:     %s\n", strings.Join(ignored, ", "))
		}
		sb.WriteString("\n")
	}

	divergences := buildTakeoverDivergences(s)
	if len(divergences) > 0 || len(s.ExistingStateConflicts) > 0 {
		sb.WriteString("Conflicts\n")
		sb.WriteString("─────────\n")
		for _, d := range divergences {
			_, _ = fmt.Fprintf(&sb, "  ! %s\n", d)
		}
		for _, c := range s.ExistingStateConflicts {
			_, _ = fmt.Fprintf(&sb, "  ! %s\n", c)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Files to create:\n")
	sb.WriteString("  .lucy/config.toml\n")
	sb.WriteString("  .lucy/manifest.toml\n")
	sb.WriteString("  .lucy/lock.json\n")

	return sb.String()
}

func buildTakeoverDivergences(s *InitFlowState) []string {
	hints := s.DiscoveredDefaults.ExistingLucy
	if !hints.HasAny() {
		return nil
	}
	obs := s.DiscoveredDefaults

	var divergences []string

	if obs.GameVersion != "" && hints.GameVersion != "" && obs.GameVersion != hints.GameVersion {
		divergences = append(divergences, fmt.Sprintf(
			"Game version: observed %q but existing manifest says %q — will use %q",
			obs.GameVersion, hints.GameVersion, s.GameVersion,
		))
	}

	if obs.Platform != "" && hints.Platform != "" && obs.Platform != hints.Platform {
		divergences = append(divergences, fmt.Sprintf(
			"Runtime: observed %q but existing manifest says %q — will use %q",
			obs.Platform, hints.Platform, s.Platform,
		))
	}

	if obs.PlatformVersion != "" && hints.PlatformVersion != "" && obs.PlatformVersion != hints.PlatformVersion {
		divergences = append(divergences, fmt.Sprintf(
			"Loader version: observed %q but existing manifest says %q — will use %q",
			obs.PlatformVersion, hints.PlatformVersion, s.PlatformVersion,
		))
	}

	return divergences
}

func conflictModeVerb(mode ConflictMode) string {
	switch mode {
	case OverwriteAll:
		return "overwritten"
	case AbortOnConflict:
		return "preserved (abort if any exist)"
	default:
		return "preserved"
	}
}

func isUserAbort(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, huh.ErrUserAborted)
}

func compatiblePlatformLabel(platform string) string {
	switch platform {
	case "fabric":
		return "Fabric compatibility – allow Fabric-targeted content through a bridge/runtime layer"
	case "mcdr":
		return "MCDR – independent controller/plugin framework"
	case "sinytra":
		return "Sinytra – NeoForge bridge layer for Fabric compatibility"
	default:
		return platform
	}
}

func buildPackageClassificationDescription(s *InitFlowState) string {
	if len(s.PackageClassifications) == 0 {
		return "No discovered packages need classification."
	}

	var sb strings.Builder
	sb.WriteString("Lucy built a package graph from the current server before writing the manifest. Non-leaf nodes are shown as dependencies only; they do not get a separate persistent role.\n\n")
	for _, classification := range s.PackageClassifications {
		_, _ = fmt.Fprintf(&sb, "- %s\n", packageClassificationLabel(classification))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func packageClassificationLabel(classification TakeoverPackageClassification) string {
	label := fmt.Sprintf("[%s] %s@%s", packageClassificationKind(classification), classification.ID, classification.Version)
	if len(classification.RequiredBy) > 0 {
		label += fmt.Sprintf(" <- %s", strings.Join(classification.RequiredBy, ", "))
	}
	return label
}

func packageClassificationKind(classification TakeoverPackageClassification) string {
	if classification.Leaf {
		return "leaf"
	}
	return "dependency"
}
