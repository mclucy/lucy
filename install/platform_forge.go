package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/workspace"
)

func installForgePlatform(
	resolved types.VersionedPackageRef,
	fetched upstream.FetchResult,
	serverDir string,
) error {
	if err := guardForgeServerTopology(); err != nil {
		return err
	}

	serverInfo := workspace.ServerInfo()
	workPath := serverDir
	if workPath == "" {
		workPath = serverInfo.Root
	}
	if workPath == "" {
		return errors.New("server working directory not found")
	}

	if err := checkJavaAvailability(); err != nil {
		return err
	}

	if err := ensureEULAAccepted(workPath); err != nil {
		return err
	}

	promptSupportProject()

	tracker := progress.NewTrackerWithLogging(resolved.StringFull(), 5)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = progress.WaitForShutdown(ctx)
	}()
	defer tracker.Close()

	result, err := cache.CachedDownload(
		fetched.FileURL,
		workPath,
		cache.DownloadOptions{
			Kind:               cache.KindArtifact,
			WrapReader:         tracker.ProxyReader,
			OnCacheHit:         tracker.CacheHit,
			OnResolvedFilename: func(title string) { tracker.SetTitle(title) },
			FileMode:           0o750,
			Filename:           fetched.Filename,
		},
	)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if result == nil {
		return errors.New("download result is nil")
	}
	defer func() { _ = result.File.Close() }()

	if err := runInstallerJar(result.File.Name(), tracker); err != nil {
		return err
	}

	tracker.SetPercent(0.99)
	if err := verifyForgeInstallation(workPath); err != nil {
		return err
	}

	workspace.Rebuild()
	tracker.Complete("Forge installed")
	return nil
}

func guardForgeServerTopology() error {
	serverPlatform := workspace.ServerInfo().Runtime.DerivedModLoader()

	switch serverPlatform {
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return fmt.Errorf(
			"found an existing server platform %s, installation of forge aborted",
			serverPlatform.Title(),
		)
	}
	return nil
}

func promptSupportProject() {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Supporting the Forge project").
				Description(
					"The Forge project is sustained by ads on the download page. By automating " +
						"this process, we may reduce ad revenue that supports the project. If you find " +
						"Forge useful, please consider supporting the project by downloading manually " +
						"from their official site <https://files.minecraftforge.net>, or support them on " +
						"Patreon at <https://www.patreon.com/LexManos>",
				),
		),
	).WithWidth(80)
	_ = form.Run()
}

func verifyForgeInstallation(workPath string) error {
	librariesPath := filepath.Join(workPath, "libraries")
	if _, err := os.Stat(librariesPath); err == nil {
		launchScripts := []string{
			"run.sh", "run.bat", "unix_args.txt", "win_args.txt",
		}
		for _, script := range launchScripts {
			if _, err := os.Stat(filepath.Join(workPath, script)); err == nil {
				return nil
			}
		}
	}

	entries, err := os.ReadDir(workPath)
	if err != nil {
		return fmt.Errorf(
			"verify forge installation failed: cannot read work directory: %w",
			err,
		)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, "forge-") && strings.HasSuffix(name, ".jar") {
			return nil
		}
	}

	return errors.New("forge installation verification failed: no artifacts found (expected libraries/ with launch scripts or forge-*.jar)")
}
