package create

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
)

// applyAccessible sets accessible mode on a form when stdin is not a TTY.
// Accessible mode bypasses bubbletea entirely (plain line reads),
// preventing hangs on Windows PowerShell where bubbletea's raw-mode console
// setup blocks.
func applyAccessible(f *huh.Form) *huh.Form {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return f.WithAccessible(true)
	}
	return f
}

// confirmProceed asks the user to confirm a risky creation. --force skips this.
func confirmProceed(title, description string, force bool) (bool, error) {
	if force {
		return true, nil
	}
	var agreed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Description(description).
				Affirmative("Yes, proceed").
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

func gatherInputs(
	coresFlag, mcFlag string,
) (rawCores, gameVersion string, cancelled bool, err error) {
	rawCores = strings.TrimSpace(coresFlag)
	gameVersion = strings.TrimSpace(mcFlag)

	if gameVersion == "" && !term.IsTerminal(os.Stdin.Fd()) {
		return "", "", false, fmt.Errorf(
			"--minecraft is required when stdin is not a terminal",
		)
	}

	var fields []huh.Field
	if rawCores == "" && term.IsTerminal(os.Stdin.Fd()) {
		fields = append(fields, huh.NewInput().
			Title("Server cores").
			Description("Space-separated. Leave empty for vanilla.").
			Value(&rawCores))
	}
	if gameVersion == "" {
		fields = append(fields, huh.NewInput().
			Title("Minecraft version").
			Description("Target Minecraft version (e.g. 1.21.4)").
			Value(&gameVersion).
			Validate(nonEmpty("a Minecraft version is required")))
	}
	if len(fields) == 0 {
		return rawCores, gameVersion, false, nil
	}

	form := huh.NewForm(huh.NewGroup(fields...))
	if err := applyAccessible(form).Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", "", true, nil
		}
		return "", "", false, err
	}
	return rawCores, gameVersion, false, nil
}

// nonEmpty builds a validator that rejects blank answers with message.
func nonEmpty(message string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(message)
		}
		return nil
	}
}
