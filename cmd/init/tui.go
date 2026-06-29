package init

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
	"github.com/mclucy/lucy/state"
)

// applyAccessible sets accessible mode on a form when stdin is not a TTY.
// Accessible mode bypasses bubbletea entirely (plain line reads), preventing
// hangs on Windows PowerShell where bubbletea's raw-mode console setup blocks.
func applyAccessible(f *huh.Form) *huh.Form {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return f.WithAccessible(true)
	}
	return f
}

// RunInteractiveInit walks the user through a minimal interactive init flow via
// huh forms, populating s in-place. Sets s.Aborted=true on cancellation at any
// step, s.Confirmed=true on final approval. No file I/O occurs here.
//
// The flow is intentionally a thin scaffold: welcome → game version → primary
// platform → compatible platforms → platform loader version → review. Richer
// takeover/discovery UX will be layered back on during the redesign.
func RunInteractiveInit(s *InitFlowState) error {
	if err := runWelcome(s); err != nil {
		return err
	}
	if s.Aborted {
		return nil
	}
	if err := runConflictResolution(s); err != nil {
		return err
	}
	if s.Aborted {
		return nil
	}
	if err := runGameVersion(s); err != nil {
		return err
	}
	if err := runEcosystem(s); err != nil {
		return err
	}
	if err := runCompatiblePlatforms(s); err != nil {
		return err
	}
	if err := runEcosystemVersion(s); err != nil {
		return err
	}
	return runReview(s)
}

func runWelcome(s *InitFlowState) error {
	var continueInit bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Welcome to Lucy").
				Description(
					"lucy init sets up a Lucy-managed Minecraft server environment in the\n"+
						"current directory. It will create the following files:\n\n"+
						"  lucy.yaml      – environment intent (game version, runtime, packages)\n"+
						"  lucy-lock.yaml – resolved facts (versions, hashes, install paths)\n\n"+
						"No files will be written until you confirm at the final review step.",
				),
			huh.NewConfirm().
				Title("Continue with setup?").
				Affirmative("Yes, let's go").
				Negative("Cancel").
				Value(&continueInit),
		),
	)
	if err := applyAccessible(form).Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("welcome step: %w", err)
	}
	if !continueInit {
		s.Aborted = true
	}
	return nil
}

func runConflictResolution(s *InitFlowState) error {
	if len(s.ExistingFiles) == 0 {
		return nil
	}

	desc := fmt.Sprintf(
		"The following Lucy files already exist in this directory:\n\n  %s\n\n"+
			"How should lucy init handle them?",
		strings.Join(s.ExistingFiles, "\n  "),
	)
	mode := string(s.ConflictResolution)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Existing Files Detected").
				Description(desc),
			huh.NewSelect[string]().
				Title("Conflict resolution").
				Options(
					huh.NewOption(
						"Keep existing files, only scaffold missing ones (recommended)",
						string(PreserveExisting),
					),
					huh.NewOption(
						"Abort if any file exists – do nothing",
						string(AbortOnConflict),
					),
					huh.NewOption(
						"Overwrite everything – replace all existing files",
						string(OverwriteAll),
					),
				).
				Value(&mode),
		),
	)
	if err := applyAccessible(form).Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("conflict resolution step: %w", err)
	}

	s.ConflictResolution = ConflictMode(mode)
	if s.ConflictResolution == AbortOnConflict {
		s.Aborted = true
		fmt.Println("\nInit aborted: existing files would be overwritten. Use --conflict=overwrite to replace them.")
	}
	return nil
}

func runGameVersion(s *InitFlowState) error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Minecraft game version").
				Description("Enter the Minecraft server version this environment targets (e.g. 1.21.4).").
				Placeholder("1.21.4").
				Validate(
					func(v string) error {
						if strings.TrimSpace(v) == "" {
							return errors.New("game version is required")
						}
						return nil
					},
				).
				Value(&s.GameVersion),
		),
	)
	if err := applyAccessible(form).Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("game version step: %w", err)
	}
	s.GameVersion = strings.TrimSpace(s.GameVersion)
	return nil
}

func runEcosystem(s *InitFlowState) error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Primary runtime").
				Description("Choose the main server runtime Lucy should treat as the primary host environment.").
				Options(
					huh.NewOption(
						"Fabric – lightweight, fast-updating mod loader",
						"fabric",
					),
					huh.NewOption(
						"NeoForge – community fork of Forge (recommended for 1.20.2+)",
						"neoforge",
					),
					huh.NewOption("Forge – original mod loader", "forge"),
					huh.NewOption(
						"MCDR – independent controller/plugin framework",
						"mcdr",
					),
					huh.NewOption("None / Vanilla – no modding platform", "none"),
				).
				Value(&s.Ecosystem),
		),
	)
	if err := applyAccessible(form).Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("platform step: %w", err)
	}
	return nil
}

func runCompatiblePlatforms(s *InitFlowState) error {
	options := state.CompatiblePlatformOptions(s.Ecosystem)
	if len(options) == 0 {
		s.CompatiblePlatforms = nil
		return nil
	}

	selected := append([]string(nil), s.CompatiblePlatforms...)
	fields := make([]huh.Option[string], 0, len(options))
	for _, platform := range options {
		fields = append(
			fields,
			huh.NewOption(compatibleEcosystemLabel(platform), platform),
		)
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Additional compatible platforms").
				Description("Select extra compatibility layers Lucy should record alongside the primary runtime. Only valid combinations for the chosen runtime are shown.").
				Options(fields...).
				Value(&selected),
		),
	)
	if err := applyAccessible(form).Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("compatible platforms step: %w", err)
	}
	s.CompatiblePlatforms = selected
	if err := ValidateEcosystemSelection(
		s.Ecosystem,
		s.CompatiblePlatforms,
	); err != nil {
		return fmt.Errorf("platform selection step: %w", err)
	}
	return nil
}

func runEcosystemVersion(s *InitFlowState) error {
	if s.Ecosystem == "" || s.Ecosystem == "none" {
		s.EcosystemVersion = ""
		return nil
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Platform loader version").
				Description(
					fmt.Sprintf(
						"Enter the %s loader version, or leave blank to use the latest.",
						s.Ecosystem,
					),
				).
				Placeholder("latest").
				Value(&s.EcosystemVersion),
		),
	)
	if err := applyAccessible(form).Run(); err != nil {
		if isUserAbort(err) {
			s.Aborted = true
			return nil
		}
		return fmt.Errorf("platform version step: %w", err)
	}
	s.EcosystemVersion = strings.TrimSpace(s.EcosystemVersion)
	return nil
}

func runReview(s *InitFlowState) error {
	var confirmWrite bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Review – Ready to initialize").
				Description(buildSummary(s)),
			huh.NewConfirm().
				Title("Write these files?").
				Affirmative("Yes, initialize").
				Negative("Cancel").
				Value(&confirmWrite),
		),
	)
	if err := applyAccessible(form).Run(); err != nil {
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
	return nil
}

func buildSummary(s *InitFlowState) string {
	var sb strings.Builder
	sb.WriteString("Proposed manifest intent\n")
	sb.WriteString("────────────────────────\n")
	fmt.Fprintf(&sb, "  Game version:  %s\n", s.GameVersion)
	if s.Ecosystem == "" || s.Ecosystem == "none" {
		sb.WriteString("  Primary runtime: none (vanilla)\n")
	} else {
		fmt.Fprintf(&sb, "  Primary runtime: %s\n", s.Ecosystem)
		if s.EcosystemVersion != "" {
			fmt.Fprintf(&sb, "  Loader version:  %s\n", s.EcosystemVersion)
		} else {
			sb.WriteString("  Loader version:  (latest)\n")
		}
	}
	if len(s.CompatiblePlatforms) > 0 {
		fmt.Fprintf(
			&sb,
			"  Compatible with: %s\n",
			strings.Join(s.CompatiblePlatforms, ", "),
		)
	}
	fmt.Fprintf(&sb, "  Conflict mode:   %s\n", s.ConflictResolution)
	if len(s.ExistingFiles) > 0 {
		fmt.Fprintf(
			&sb, "  Existing files:  %s (will be %s)\n",
			strings.Join(s.ExistingFiles, ", "),
			conflictModeVerb(s.ConflictResolution),
		)
	}
	sb.WriteString("\nFiles to create:\n")
	sb.WriteString("  lucy.yaml\n")
	sb.WriteString("  lucy-lock.yaml\n")
	return sb.String()
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
	return err != nil && errors.Is(err, huh.ErrUserAborted)
}

func compatibleEcosystemLabel(platform string) string {
	switch platform {
	case "fabric":
		return "Fabric compatibility – allow Fabric-targeted content through a bridge/runtime layer"
	case "mcdr":
		return "MCDR – independent controller / plugin framework"
	case "sinytra":
		return "Sinytra – NeoForge bridge layer for Fabric compatibility"
	default:
		return platform
	}
}
