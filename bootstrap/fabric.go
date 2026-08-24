package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

type fabricBootstrapper struct{}

func (b fabricBootstrapper) Bootstrap(
	_ context.Context,
	fetched types.ResolvedPackage,
	serverDir string,
) error {
	ws := workspace.New()
	serverLoader := selectedLoader(ws.Server)

	deleteVanilla := false
	switch serverLoader {
	case types.EcoFabric:
		return errors.New("fabric server already detected, installation aborted")
	case types.EcoForge:
		return errors.New("forge server detected, cannot install fabric bootstrap")
	case types.EcoNeoforge:
		return errors.New("neoforge server detected, cannot install fabric bootstrap")
	case types.EcoUnspecified:
	default:
		return fmt.Errorf(
			"unsupported selected loader %s for fabric installation",
			serverLoader.Title(),
		)
	}
	if isVanillaServer(ws.Server) {
		override, shouldDeleteVanilla := promptOverrideVanilla(ws.Server)
		if !override {
			return errors.New("installation aborted by user")
		}
		deleteVanilla = shouldDeleteVanilla

	}

	workPath := serverDir
	if workPath == "" {
		workPath = ws.Root
	}
	if workPath == "" {
		workPath = "."
	}

	tracker := progress.NewTracker("fabric")
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
			Kind:               cache.KindArtifact,
			Filename:           fetched.Filename,
			WrapReader:         tracker.ProxyReader,
			OnCacheHit:         tracker.CacheHit,
			OnResolvedFilename: func(title string) { tracker.SetTitle(title) },
		},
	)
	if result != nil {
		fn.CloseReader(result.File, func(error) {})
	}
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	if deleteVanilla {
		if err := os.Remove(ws.Server.PrimaryPath); err != nil {
			return fmt.Errorf("delete vanilla server failed: %w", err)
		}
	}

	workspace.Rebuild()
	return nil
}

func init() {
	bootstrappers[types.EcoFabric] = fabricBootstrapper{}
}

func promptOverrideVanilla(
	server *workspace.ServerInstance,
) (override bool, deleteVanilla bool) {
	path := server.PrimaryPath
	version := server.GameVersion().String()
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Vanilla server detected, override it with a corresponding fabric server?").
				Description(
					fmt.Sprintf(
						"Found server at %s, with game version %s",
						path, version,
					),
				).
				Value(&override),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Delete vanilla server after fabric installation?").
				Description(fmt.Sprintf("Will delete %s", path)).
				Value(&deleteVanilla),
		).WithHideFunc(func() bool { return !override }),
	)
	_ = form.Run()
	return
}
