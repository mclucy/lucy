package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectStateServiceIsolatesProjects(t *testing.T) {
	ctx := context.Background()
	dirA := t.TempDir()
	dirB := t.TempDir()

	cfgA := ConfigDefaults()
	cfgA.Sources.Preferred = "github"
	manifestA := ManifestDefaults()
	manifestA.Config = &cfgA
	manifestA.Environment.GameVersion = "1.21.1"
	lockA := NewLock()
	lockA.ManifestFingerprint = "sha256:a"
	lockA.GameVersion = "1.21.1"
	lockA.Platform = "fabric"
	lockA.PlatformVersion = "0.16.10"

	serviceA := NewProjectStateService(dirA)
	if err := serviceA.Save(ctx, &manifestA, &lockA); err != nil {
		t.Fatalf("save A failed: %v", err)
	}
	if err := serviceA.Load(ctx); err != nil {
		t.Fatalf("load A failed: %v", err)
	}

	manifestB := ManifestDefaults()
	manifestB.Config = new(ConfigDefaults())
	manifestB.Environment.GameVersion = "1.20.6"
	lockB := NewLock()
	lockB.ManifestFingerprint = "sha256:b"
	lockB.GameVersion = "1.20.6"
	lockB.Platform = "neoforge"
	lockB.PlatformVersion = "21.1.0"

	serviceB := NewProjectStateService(dirB)
	if err := serviceB.Save(ctx, &manifestB, &lockB); err != nil {
		t.Fatalf("save B failed: %v", err)
	}
	if err := serviceB.Load(ctx); err != nil {
		t.Fatalf("load B failed: %v", err)
	}

	serviceB.Manifest().Config.Upgrade.Mode = "latest"
	if err := serviceB.Save(
		ctx,
		serviceB.Manifest(),
		serviceB.Lock(),
	); err != nil {
		t.Fatalf("second save B failed: %v", err)
	}
	if err := serviceB.Reload(ctx); err != nil {
		t.Fatalf("reload B failed: %v", err)
	}

	if serviceA.Config() == nil || serviceB.Config() == nil {
		t.Fatal("expected both services to hold configs")
	}
	if serviceA.Config().Upgrade.Mode != "compatible" {
		t.Fatalf(
			"expected A to remain unchanged, got %q",
			serviceA.Config().Upgrade.Mode,
		)
	}
	if serviceB.Config().Upgrade.Mode != "latest" {
		t.Fatalf(
			"expected B mutation to persist, got %q",
			serviceB.Config().Upgrade.Mode,
		)
	}
	if serviceA.Manifest().Environment.GameVersion != "1.21.1" {
		t.Fatalf(
			"expected A manifest to remain isolated, got %q",
			serviceA.Manifest().Environment.GameVersion,
		)
	}
	if serviceB.Manifest().Environment.GameVersion != "1.20.6" {
		t.Fatalf(
			"expected B manifest to remain isolated, got %q",
			serviceB.Manifest().Environment.GameVersion,
		)
	}
}

func TestProjectStateServiceLoadMissingFilesReturnsNilState(t *testing.T) {
	service := NewProjectStateService(t.TempDir())
	if err := service.Load(context.Background()); err != nil {
		t.Fatalf("expected missing files to be allowed, got %v", err)
	}
	if service.Config() != nil || service.Manifest() != nil || service.Lock() != nil {
		t.Fatal("expected all state pointers to remain nil when files are missing")
	}
}

func TestProjectStateServiceSaveThenReloadReadsManifestConfigBack(t *testing.T) {
	ctx := context.Background()
	service := NewProjectStateService(t.TempDir())
	cfg := ConfigDefaults()
	cfg.Sources.Preferred = "mcdr"
	manifest := ManifestDefaults()
	manifest.Config = &cfg

	if err := service.Save(ctx, &manifest, nil); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	service.Invalidate()

	if err := service.Reload(ctx); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if service.Config() == nil {
		t.Fatal("expected config after reload")
	}
	if service.Config().Sources.Preferred != "mcdr" {
		t.Fatal("expected saved config to round-trip through reload")
	}
	if service.Manifest() == nil {
		t.Fatal("expected lucy.yaml manifest")
	}
	if service.Lock() != nil {
		t.Fatal("expected lock file to remain nil")
	}
}

func TestProjectStateServiceLoadMergesGlobalConfigUnderWorkspaceConfig(t *testing.T) {
	ctx := context.Background()
	configHome := t.TempDir()
	t.Setenv("HOME", configHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(configHome, "xdg"))

	globalCfg := ConfigDefaults()
	globalCfg.Sources.Preferred = "curseforge"
	globalCfg.Upgrade.Mode = "latest"
	if err := WriteGlobalConfig(&globalCfg); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	workspaceDir := t.TempDir()
	workspaceCfg := Config{
		Sources: SourcesConfig{
			Preferred: "modrinth",
		},
	}
	manifest := ManifestDefaults()
	manifest.Config = &workspaceCfg
	if err := WriteManifest(workspaceDir, &manifest); err != nil {
		t.Fatalf("write workspace manifest: %v", err)
	}

	service := NewProjectStateService(workspaceDir)
	if err := service.Load(ctx); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if service.Config() == nil {
		t.Fatal("expected merged config")
	}
	if got := service.Config().Sources.Preferred; got != "modrinth" {
		t.Fatalf(
			"expected workspace preferred source to override global, got %q",
			got,
		)
	}
	if got := service.Config().Upgrade.Mode; got != "latest" {
		t.Fatalf("expected global upgrade mode to be preserved, got %q", got)
	}
	if got := service.Config().Sources.Priority; len(got) != len(globalCfg.Sources.Priority) {
		t.Fatalf("expected global source priority to be preserved, got %v", got)
	}
}

func TestProjectStateServiceLoadRejectsEmptyWorkDir(t *testing.T) {
	err := NewProjectStateService("").Load(context.Background())
	if err == nil {
		t.Fatal("expected empty workDir to fail")
	}

	var stateErr StateError
	if !errors.As(err, &stateErr) {
		t.Fatalf("expected StateError, got %T", err)
	}
	if stateErr.Kind != ErrIOFailure {
		t.Fatalf("expected IO failure, got %q", stateErr.Kind)
	}
	if stateErr.Field != "workDir" {
		t.Fatalf("expected workDir field, got %q", stateErr.Field)
	}
}

func TestProjectStateServiceLoadRejectsMalformedExistingFile(t *testing.T) {
	workDir := t.TempDir()
	malformedConfig := []byte("format_version: v1\nenvironment: {}\npackages: []\nbundles: []\nconfig:\n  sources:\n    priority:\n      - invalid\n    preferred: auto\n  upgrade:\n    mode: compatible\n")
	if err := os.WriteFile(
		filepath.Join(workDir, string(ManifestFile)),
		malformedConfig,
		0o644,
	); err != nil {
		t.Fatalf("write malformed config failed: %v", err)
	}

	err := NewProjectStateService(workDir).Load(context.Background())
	if err == nil {
		t.Fatal("expected malformed config to fail load")
	}

	var stateErr StateError
	if !errors.As(err, &stateErr) {
		t.Fatalf("expected StateError, got %T", err)
	}
	if stateErr.File != ManifestFile {
		t.Fatalf("expected config file error, got %q", stateErr.File)
	}
	if stateErr.Kind != ErrMalformed {
		t.Fatalf("expected malformed error, got %q", stateErr.Kind)
	}
}
