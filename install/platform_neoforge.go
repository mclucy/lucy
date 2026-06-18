package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/workspace"
)

func installNeoforgePlatform(
	resolved types.VersionedPackageRef,
	fetched upstream.FetchResult,
	serverDir string,
) error {
	if err := guardNeoforgeServerTopology(); err != nil {
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
	if err := verifyNeoforgeInstallation(workPath); err != nil {
		return err
	}

	workspace.Rebuild()
	tracker.Complete("NeoForge installed")
	return nil
}

func guardNeoforgeServerTopology() error {
	serverPlatform := workspace.ServerInfo().Runtime.DerivedModLoader()

	switch serverPlatform {
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return fmt.Errorf(
			"found an existing server platform %s, installation of NeoForge aborted",
			serverPlatform.Title(),
		)
	}
	return nil
}

func verifyNeoforgeInstallation(workPath string) error {
	launchScripts := []string{"run.sh", "run.bat"}
	for _, script := range launchScripts {
		if _, err := os.Stat(filepath.Join(workPath, script)); err == nil {
			return nil
		}
	}

	neoLibPath := filepath.Join(workPath, "libraries", "net", "neoforged")
	if _, err := os.Stat(neoLibPath); err == nil {
		return nil
	}

	return errors.New(
		"NeoForge installation verification failed: no artifacts found " +
			"(expected run.sh/run.bat or libraries/net/neoforged/)",
	)
}
