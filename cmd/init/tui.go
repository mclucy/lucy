package init

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
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
						"  .lucy/manifest.toml – environment intent (game version, platform, mods)\n"+
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
				Title("Server platform").
				Description("Choose the modding platform or server core for this environment.").
				Options(
					huh.NewOption("Fabric – lightweight, fast-updating mod loader", "fabric"),
					huh.NewOption("NeoForge – community fork of Forge (recommended for 1.20.2+)", "neoforge"),
					huh.NewOption("Forge – original mod loader", "forge"),
					huh.NewOption("MCDR – plugin framework for vanilla/Bukkit servers", "mcdr"),
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

	_, _ = fmt.Fprintf(&sb, "Game version:    %s\n", s.GameVersion)

	if s.Platform == "" || s.Platform == "none" {
		sb.WriteString("Platform:        none (vanilla)\n")
	} else {
		_, _ = fmt.Fprintf(&sb, "Platform:        %s\n", s.Platform)
		if s.PlatformVersion != "" {
			_, _ = fmt.Fprintf(&sb, "Loader version:  %s\n", s.PlatformVersion)
		} else {
			sb.WriteString("Loader version:  (latest)\n")
		}
	}

	if len(s.ManagedRoots) > 0 {
		_, _ = fmt.Fprintf(&sb, "Managed dirs:    %s\n", strings.Join(s.ManagedRoots, ", "))
	} else {
		sb.WriteString("Managed dirs:    (none selected)\n")
	}

	_, _ = fmt.Fprintf(&sb, "Conflict mode:   %s\n", s.ConflictResolution)

	if len(s.ExistingFiles) > 0 {
		_, _ = fmt.Fprintf(&sb, "\nExisting files:  %s\n", strings.Join(s.ExistingFiles, ", "))
	}
	if len(s.ExistingStateConflicts) > 0 {
		_, _ = fmt.Fprintf(&sb, "Conflicts:       %s\n", strings.Join(s.ExistingStateConflicts, "; "))
	}
	if s.DiscoveredDefaults.Confidence != ConfidenceNone {
		_, _ = fmt.Fprintf(&sb, "Discovery:       %s\n", describeDiscovery(s.DiscoveredDefaults))
		if len(s.DiscoveredDefaults.DetectedPackages) > 0 {
			_, _ = fmt.Fprintf(&sb, "Detected pkgs:   %s\n", strings.Join(s.DiscoveredDefaults.DetectedPackages, ", "))
		}
	}

	sb.WriteString("\nFiles to create:\n")
	sb.WriteString("  .lucy/config.toml\n")
	sb.WriteString("  .lucy/manifest.toml\n")
	sb.WriteString("  .lucy/lock.json\n")

	return sb.String()
}

func isUserAbort(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, huh.ErrUserAborted)
}
