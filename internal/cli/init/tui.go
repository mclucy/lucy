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

// applyAccessible switches a form to accessible mode when stdin is not a
// terminal. Accessible mode reads plain input lines. It bypasses
// bubbletea and avoids raw-mode hangs on Windows PowerShell.
func applyAccessible(f *huh.Form) *huh.Form {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return f.WithAccessible(true)
	}
	return f
}

// confirm asks one yes/no question. An abort counts as a decline.
func confirm(title, description string) (bool, error) {
	var agreed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description(description).
				Affirmative("Yes").
				Negative("No").
				Value(&agreed),
		),
	)
	if err := applyAccessible(form).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, nil
		}
		return false, err
	}
	return agreed, nil
}

// confirmManifestWrite shows the manifest and asks for approval.
// An abort counts as a decline.
func confirmManifestWrite(mf *state.Manifest) (bool, error) {
	var write bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Review – Ready to initialize").
				Description(manifestSummary(mf)),
			huh.NewConfirm().
				Title("Write lucy.yaml?").
				Affirmative("Yes, initialize").
				Negative("Cancel").
				Value(&write),
		),
	)
	if err := applyAccessible(form).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, nil
		}
		return false, err
	}
	return write, nil
}

// manifestSummary renders the manifest environment for review.
func manifestSummary(mf *state.Manifest) string {
	env := mf.Environment
	var sb strings.Builder
	sb.WriteString("Proposed manifest intent\n")
	sb.WriteString("────────────────────────\n")
	_, _ = fmt.Fprintf(&sb, "  Game version:     %s\n", displayOrNone(env.GameVersion))
	if env.ModdingPlatform == "" {
		sb.WriteString("  Modding platform: none\n")
	} else {
		_, _ = fmt.Fprintf(&sb, "  Modding platform: %s\n", env.ModdingPlatform)
		_, _ = fmt.Fprintf(
			&sb,
			"  Platform version: %s\n",
			displayOrNone(env.ModdingPlatformVersion),
		)
	}
	if env.ServerCore != "" {
		_, _ = fmt.Fprintf(&sb, "  Server core:      %s\n", env.ServerCore)
		_, _ = fmt.Fprintf(
			&sb,
			"  Core version:     %s\n",
			displayOrNone(env.ServerCoreVersion),
		)
	}
	if env.Mcdr {
		sb.WriteString("  MCDR:             yes\n")
	}
	for _, p := range env.CompatiblePlatforms {
		_, _ = fmt.Fprintf(&sb, "  Compatible:       %s\n", p)
	}
	return sb.String()
}

// displayOrNone replaces an empty string with a visible marker.
func displayOrNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
