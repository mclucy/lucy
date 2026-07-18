package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

type mojangBootstrapper struct{}

func (b mojangBootstrapper) Bootstrap(
	_ context.Context,
	fetched types.ResolvedPackage,
	serverDir string,
) error {
	if server := workspace.New().Server; server != nil && server.IsValid() {
		return errors.New("a server is already installed")
	}

	workPath := serverDir
	if workPath == "" {
		workPath = workspace.New().Root
	}
	if workPath == "" {
		workPath = "."
	}

	if err := ensureEULAAccepted(workPath); err != nil {
		return err
	}

	tracker := progress.NewTracker("Downloading server")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = progress.WaitForShutdown(ctx)
	}()
	defer tracker.Close()

	result, err := cache.CachedDownload(
		fetched.FileUrl,
		workPath,
		cache.DownloadOptions{
			Kind:          cache.KindArtifact,
			ExpectedHash:  fetched.Hash,
			HashAlgorithm: cache.ParseHashAlgorithm(fetched.HashAlgorithm),
			Filename:      fetched.Filename,
			WrapReader:    tracker.ProxyReader,
			OnResolvedFilename: func(name string) {
				tracker.SetTitle(name)
			},
			OnCacheHit: func() {
				tracker.Complete("cache hit")
				time.Sleep(500 * time.Millisecond)
			},
		},
	)
	if err != nil {
		return fmt.Errorf("download minecraft server jar failed: %w", err)
	}
	if result == nil {
		return errors.New("download result is nil")
	}
	defer func() { _ = result.File.Close() }()

	if err := addExecutePermission(result.File); err != nil {
		return err
	}

	workspace.Rebuild()
	return nil
}

func init() {
	bootstrappers[types.EcoMinecraft] = mojangBootstrapper{}
}

func addExecutePermission(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("read server jar file mode failed: %w", err)
	}

	mode := info.Mode()
	if mode&0o111 == 0o111 {
		return nil
	}

	if err := file.Chmod(mode | 0o111); err != nil {
		return fmt.Errorf(
			"set execute permission on server jar failed: %w",
			err,
		)
	}

	return nil
}
