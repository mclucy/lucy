package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

type mcdrBootstrapper struct{}

func (b mcdrBootstrapper) Bootstrap(
	ctx context.Context,
	_ types.ResolvedPackage,
	serverDir string,
) error {
	if workspace.New().Environments.Mcdr != nil {
		return errors.New("mcdr already installed")
	}

	workPath := serverDir
	if workPath == "" {
		workPath = workspace.New().Root
	}
	if workPath == "" {
		workPath = "."
	}

	log.ShowInfo("Checking for mcdreforged on PATH")
	if err := checkMCDRCLI(ctx); err != nil {
		return err
	}

	log.ShowInfo("Preparing MCDReforged layout")
	if err := os.Mkdir(
		filepath.Join(workPath, "server"),
		0o755,
	); err != nil && !os.IsExist(err) {
		return fmt.Errorf("create server directory failed: %w", err)
	}

	files, err := os.ReadDir(workPath)
	if err != nil {
		return fmt.Errorf("read server directory failed: %w", err)
	}

	movable := make([]os.DirEntry, 0, len(files))
	for _, file := range files {
		if file.Name() == "server" {
			continue
		}
		movable = append(movable, file)
	}

	if len(movable) > 0 {
		log.ShowInfo(
			fmt.Sprintf(
				"Moving %d item(s) into server/",
				len(movable),
			),
		)
	}
	for _, file := range movable {
		src := filepath.Join(workPath, file.Name())
		dst := filepath.Join(workPath, "server", file.Name())
		log.Info("mcdr bootstrap: " + src + " -> " + dst)
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s into server/: %w", file.Name(), err)
		}
	}

	log.ShowInfo("Running mcdreforged init in " + workPath)
	out, err := runMCDRInit(ctx, workPath)
	if err != nil {
		return err
	}
	if trimmed := strings.TrimSpace(out); trimmed != "" {
		log.ShowInfo(trimmed)
	}

	workspace.Rebuild()
	log.ShowInfo("MCDReforged installed")
	return nil
}

func checkMCDRCLI(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "mcdreforged", "--version")
	versionOut, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"mcdreforged not available (install MCDReforged and ensure `mcdreforged` is on PATH): %w",
			err,
		)
	}
	versionLine := strings.TrimSpace(string(versionOut))
	if nl := strings.IndexByte(versionLine, '\n'); nl >= 0 {
		versionLine = versionLine[:nl]
	}
	log.Info("mcdr bootstrap: mcdreforged " + versionLine)
	return nil
}

func runMCDRInit(ctx context.Context, workDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "mcdreforged", "init")
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		return text, fmt.Errorf(
			"mcdreforged init failed: %w\n%s",
			err,
			strings.TrimSpace(text),
		)
	}
	log.Info("mcdr bootstrap: init output:\n" + text)
	return text, nil
}

func init() {
	bootstrappers[types.PlatformMCDR] = mcdrBootstrapper{}
}
