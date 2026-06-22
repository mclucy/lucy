package bootstrap

import (
	"context"
	"errors"
	"os"
	"os/exec"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

type mcdrBootstrapper struct{}

func (b mcdrBootstrapper) Bootstrap(_ context.Context, _ types.ResolvedPackage, _ string) error {
	if workspace.ServerInfo().Environments.Mcdr != nil {
		return errors.New("mcdr already installed")
	}

	if err := exec.Command("mcdreforged", "--version").Run(); err != nil {
		return err
	}

	if err := os.Mkdir("server", 0o755); err != nil {
		return err
	}

	files, err := os.ReadDir(".")
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.Name() == "server" {
			continue
		}
		if err := os.Rename(file.Name(), "server/"+file.Name()); err != nil {
			return err
		}
	}

	if err := exec.Command("mcdreforged", "init").Run(); err != nil {
		return err
	}

	workspace.Rebuild()
	return nil
}

func init() {
	bootstrappers[types.PlatformMCDR] = mcdrBootstrapper{}
}
