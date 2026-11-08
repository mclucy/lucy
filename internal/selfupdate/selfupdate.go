package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
)

// DefaultManifestURL is the promoted release manifest served by Cloudflare.
const DefaultManifestURL = "https://lucy.lc/latest.json"

// Doer performs one HTTP request. It is satisfied by *http.Client and by the
// cache client's uncached transport.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Options selects the executable and the release the command operates on.
type Options struct {
	// ManifestURL overrides DefaultManifestURL. Tests and mirrors use it.
	ManifestURL string
	// Executable overrides the running binary path. Tests use it.
	Executable string
	// Version, Commit, and Official describe the running build.
	Version  string
	Commit   string
	Official bool
	// Force reinstalls the selected release even when it is not newer.
	Force bool
	// Progress reports the archive transfer in bytes.
	Progress func(name string, read, total int64)
}

// Result reports what the manifest offered and what the command did.
type Result struct {
	CurrentVersion  string
	CurrentCommit   string
	LatestVersion   string
	LatestChannel   string
	LatestCommit    string
	UpdateAvailable bool
	Action          string
	Asset           string
	Checksum        string
	Executable      string
	ReleaseURL      string
	Notes           []string
}

// Service resolves and applies lucy releases for one platform.
type Service struct {
	doer   Doer
	logger *log.Logger
	goos   string
	goarch string
}

// New constructs a service. logger may be nil, which records nothing.
func New(doer Doer, logger *log.Logger) *Service {
	return &Service{
		doer:   doer,
		logger: logger,
		goos:   runtime.GOOS,
		goarch: runtime.GOARCH,
	}
}

// Check resolves the manifest and reports whether an update is available. It
// never writes to the installed binary.
func (s *Service) Check(ctx context.Context, opts Options) (*Result, error) {
	resolved, err := s.resolve(ctx, opts)
	if err != nil {
		return nil, err
	}
	installation, err := s.detect(opts, false)
	if err != nil {
		return nil, err
	}

	result := resolved.result(opts)
	result.Action = "checked"
	result.Executable = installation.Executable
	result.Notes = append(result.Notes, installation.Notes...)
	return result, nil
}

// Apply resolves the manifest and replaces the running executable when the
// release is newer, or when the caller forces a reinstall.
func (s *Service) Apply(ctx context.Context, opts Options) (*Result, error) {
	installation, err := s.detect(opts, true)
	if err != nil {
		return nil, err
	}
	switch {
	case installation.Blocked:
		return nil, fmt.Errorf("this lucy cannot replace itself; %s", installation.Guidance)
	case installation.Managed:
		return nil, fmt.Errorf(
			"lucy self-update is unavailable for a %s installation; %s",
			installation.Method,
			installation.Guidance,
		)
	case !installation.Updatable && !opts.Force:
		return nil, fmt.Errorf("this lucy cannot replace itself; %s", installation.Guidance)
	}

	resolved, err := s.resolve(ctx, opts)
	if err != nil {
		return nil, err
	}
	result := resolved.result(opts)
	result.Executable = installation.Executable
	if !resolved.updateAvailable && !opts.Force {
		result.Action = "up-to-date"
		return result, nil
	}

	lock, err := acquireLock()
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := lock.Close(); closeErr != nil && s.logger != nil {
			s.logger.Warn(fmt.Errorf("self-update lock was not released: %w", closeErr))
		}
	}()

	s.logDebug("self-update started",
		"current", opts.Version,
		"latest", resolved.manifest.Version,
		"asset", resolved.file.Name,
		"executable", installation.Executable,
	)
	if err := s.downloadAndReplace(ctx, opts, resolved, installation.Executable); err != nil {
		return nil, err
	}
	result.Action = "updated"
	s.logDebug("self-update installed",
		"previous", opts.Version,
		"version", resolved.manifest.Version,
		"executable", installation.Executable,
	)
	return result, nil
}

func (s *Service) logDebug(message string, args ...any) {
	if s.logger != nil {
		s.logger.Debug(message, args...)
	}
}

func acquireLock() (*fsutil.FileLock, error) {
	lockPath := filepath.Join(filepath.Dir(cache.DefaultRoot()), "self-update.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("prepare the update lock: %w", err)
	}
	lock, err := fsutil.TryLockFile(lockPath)
	if errors.Is(err, fsutil.ErrLocked) {
		return nil, errors.New("another lucy update is already running")
	}
	if err != nil {
		return nil, fmt.Errorf("acquire the update lock: %w", err)
	}
	return lock, nil
}

func (r *resolved) result(opts Options) *Result {
	return &Result{
		CurrentVersion:  strings.TrimSpace(opts.Version),
		CurrentCommit:   opts.Commit,
		LatestVersion:   r.manifest.Version,
		LatestChannel:   r.manifest.Channel,
		LatestCommit:    r.manifest.Commit,
		UpdateAvailable: r.updateAvailable,
		Asset:           r.file.Name,
		Checksum:        r.file.SHA256,
		ReleaseURL:      r.manifest.Release,
	}
}
