package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

type ProjectStateService struct {
	workDir  string
	config   *Config
	manifest *Manifest
	lock     *Lock
	loaded   bool
}

func NewProjectStateService(workDir string) *ProjectStateService {
	return &ProjectStateService{workDir: workDir}
}

func (s *ProjectStateService) Load(ctx context.Context) error {
	if s.loaded {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.workDir == "" {
		return ioStateError("", "workDir", "workDir is required", nil)
	}

	globalCfg, _, err := ReadGlobalConfig()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(ctx, s.workDir)
	if err != nil {
		return err
	}
	cfg = MergeConfig(globalCfg, cfg)
	manifest, err := loadManifest(ctx, s.workDir)
	if err != nil {
		return err
	}
	lock, err := loadLock(ctx, s.workDir)
	if err != nil {
		return err
	}

	s.config = cfg
	s.manifest = manifest
	s.lock = lock
	s.loaded = true
	return nil
}

func (s *ProjectStateService) Reload(ctx context.Context) error {
	s.Invalidate()
	return s.Load(ctx)
}

func (s *ProjectStateService) Invalidate() {
	s.config = nil
	s.manifest = nil
	s.lock = nil
	s.loaded = false
}

func (s *ProjectStateService) Config() *Config { return s.config }

func (s *ProjectStateService) Manifest() *Manifest { return s.manifest }

func (s *ProjectStateService) Lock() *Lock { return s.lock }

func (s *ProjectStateService) Save(ctx context.Context, cfg *Config, m *Manifest, l *Lock) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.workDir == "" {
		return ioStateError("", "workDir", "workDir is required", nil)
	}

	if cfg != nil || m != nil {
		manifest, err := mergedManifest(ctx, s.workDir, cfg, m)
		if err != nil {
			return err
		}
		data, err := SerializeManifest(manifest)
		if err != nil {
			return err
		}
		if err := writeStateFile(ctx, filepath.Join(s.workDir, string(ManifestFile)), ManifestFile, data); err != nil {
			return err
		}
		cfg = manifest.Config
		m = manifest
	}
	if l != nil {
		data, err := SerializeLock(l)
		if err != nil {
			return err
		}
		if err := writeStateFile(ctx, filepath.Join(s.workDir, string(LockFile)), LockFile, data); err != nil {
			return err
		}
	}

	s.config = cfg
	s.manifest = m
	s.lock = l
	s.loaded = true
	return nil
}

func loadConfig(ctx context.Context, workDir string) (*Config, error) {
	data, err := readStateFile(ctx, filepath.Join(workDir, string(ConfigFile)), ConfigFile)
	if err != nil || data == nil {
		return nil, err
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return nil, malformedStateError(ConfigFile, "document", err)
	}
	return manifest.Config, nil
}

func loadManifest(ctx context.Context, workDir string) (*Manifest, error) {
	data, err := readStateFile(ctx, filepath.Join(workDir, string(ManifestFile)), ManifestFile)
	if err != nil || data == nil {
		return nil, err
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return nil, malformedStateError(ManifestFile, "document", err)
	}
	return manifest, nil
}

func loadLock(ctx context.Context, workDir string) (*Lock, error) {
	data, err := readStateFile(ctx, filepath.Join(workDir, string(LockFile)), LockFile)
	if err != nil || data == nil {
		return nil, err
	}
	lock, err := ParseLock(data)
	if err != nil {
		return nil, malformedStateError(LockFile, "document", err)
	}
	return lock, nil
}

func mergedManifest(ctx context.Context, workDir string, cfg *Config, m *Manifest) (*Manifest, error) {
	if m == nil {
		existing, err := loadManifest(ctx, workDir)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			defaults := ManifestDefaults()
			existing = &defaults
		}
		m = existing
	}

	if cfg == nil {
		cfg = m.Config
	}
	if cfg == nil {
		existing, err := loadConfig(ctx, workDir)
		if err != nil {
			return nil, err
		}
		cfg = existing
	}
	m.Config = cfg

	return m, nil
}

func readStateFile(ctx context.Context, path string, file StateFile) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, ioStateError(file, "document", "read failed", err)
	}
	return data, nil
}

func writeStateFile(ctx context.Context, path string, file StateFile, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ioStateError(file, "document", "mkdir failed", err)
	}
	if err := AtomicWrite(path, data, 0o644); err != nil {
		return ioStateError(file, "document", "write failed", err)
	}
	return nil
}
