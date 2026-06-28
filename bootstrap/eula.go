package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
)

const minecraftEULAURL = "https://aka.ms/MinecraftEULA"

func ensureEULAAccepted(workPath string) error {
	if hasAcceptedEULA(workPath) {
		return nil
	}

	accepted := false
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Minecraft EULA consent required").
				Description("To install and run the official server, you must agree to Mojang EULA: " + minecraftEULAURL).
				Affirmative("I Agree").
				Negative("Cancel").
				Value(&accepted),
		),
	)
	err := form.Run()
	if err != nil {
		return fmt.Errorf(
			"unable to confirm EULA acceptance interactively after reviewing %s: %w",
			minecraftEULAURL, err,
		)
	}

	if !accepted {
		return fmt.Errorf(
			"minecraft server installation aborted: EULA was not accepted (%s)",
			minecraftEULAURL,
		)
	}

	return writeEULAFile(workPath)
}

func hasAcceptedEULA(workPath string) bool {
	data, err := os.ReadFile(filepath.Join(workPath, "eula.txt"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "eula=true")
}

func writeEULAFile(workPath string) error {
	content := strings.Join(
		[]string{
			"# By changing the setting below to TRUE you are indicating your agreement to the Minecraft EULA.",
			"# " + minecraftEULAURL,
			"eula=true",
			"",
		},
		"\n",
	)
	if _, err := os.Stat(workPath); os.IsNotExist(err) {
		err = os.MkdirAll(workPath, 0o755)
		if err != nil {
			return err
		}
	}
	err := os.WriteFile(filepath.Join(workPath, "eula.txt"), []byte(content), 0o644)
	if err != nil {
		return fmt.Errorf("write eula.txt failed: %w", err)
	}
	return nil
}
