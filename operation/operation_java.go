package operation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/toolchain"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
)

// JavaChange edits the Java selection of a workspace. A nil field leaves that
// part unchanged.
type JavaChange struct {
	// Version is the major version that lucy.yaml pins; zero returns it to
	// auto.
	Version *int
	// Distribution is the distribution that lucy.yaml pins; the unspecified
	// distribution returns it to auto.
	Distribution *types.JavaDistribution
	// Path is a Java home or java executable to use on this machine; empty
	// lets Lucy choose again.
	Path *string
}

// JavaChangeResult reports the Java selection after a change.
type JavaChangeResult struct {
	// Java is the override in lucy.yaml; nil means auto.
	Java *manifest.Java `json:"java,omitempty"`
	// Home is the Java home that the local settings select; empty means Lucy
	// chooses.
	Home              string `json:"home,omitempty"`
	PublishedManifest bool   `json:"published_manifest"`
	PublishedLock     bool   `json:"published_lock"`
	PublishedLocal    bool   `json:"published_local"`
}

// SetJava applies change to the workspace in root. The local Java path is
// probed first, so an unusable path leaves lucy.yaml unchanged. A lock that
// matched the manifest before the change records the new override and stays
// current, because the Java override changes no locked artifact.
func SetJava(ctx context.Context, root string, change JavaChange) (result *JavaChangeResult, resultErr error) {
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, err
	}
	d, err := manifest.Read(root)
	if err != nil {
		return nil, err
	}
	local, err := manifest.ReadLocal(root)
	if err != nil {
		return nil, err
	}
	result = &JavaChangeResult{}
	if change.Path != nil {
		if err := setLocalJava(ctx, root, local, *change.Path); err != nil {
			return nil, err
		}
		result.PublishedLocal = true
	}
	if change.Version != nil || change.Distribution != nil {
		if err := setJavaOverride(root, d, change, result); err != nil {
			return nil, err
		}
	}
	result.Java = d.Server.Java
	result.Home = local.JavaHome(root)
	return result, nil
}

func setJavaOverride(root string, d *manifest.Document, change JavaChange, result *JavaChangeResult) error {
	before, err := manifest.Fingerprint(d)
	if err != nil {
		return err
	}
	override := manifest.Java{}
	if d.Server.Java != nil {
		override = *d.Server.Java
	}
	if change.Version != nil {
		override.Version = *change.Version
	}
	if change.Distribution != nil {
		override.Distribution = *change.Distribution
	}
	d.Server.Java = &override
	after, err := manifest.Fingerprint(d)
	if err != nil {
		return err
	}
	if after == before {
		log.Debug("java override unchanged", "reason", "lucy.yaml already declares it")
		return nil
	}
	if err := manifest.Write(root, d); err != nil {
		return err
	}
	result.PublishedManifest = true
	log.Info(
		"java override changed",
		"version", override.Version,
		"distribution", override.Distribution.String(),
	)
	lock, err := lockfile.Read(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if lock.ManifestFingerprint != before {
		log.Debug("lock left stale", "reason", "lock did not match the manifest before the change")
		return nil
	}
	lock.ManifestFingerprint = after
	lock.Server.Java = lockfile.NewJava(lock.Server.Java.Advisory, d.Server.Java)
	if err := lockfile.Write(root, lock); err != nil {
		return err
	}
	result.PublishedLock = true
	return nil
}

// setLocalJava records path in the local settings after it proves to be a
// usable Java runtime. The home keeps the spelling of path rather than its
// resolved target, so a package manager link such as
// /opt/homebrew/opt/openjdk@21 survives upgrades. A home inside the workspace
// is stored relative to it, so a bundled runtime keeps working after a move.
func setLocalJava(ctx context.Context, root string, local *manifest.Local, path string) error {
	if path == "" {
		local.Java = nil
		return manifest.WriteLocal(root, local)
	}
	home, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if info, err := os.Stat(home); err == nil && !info.IsDir() {
		// An executable path names <home>/bin/java.
		home = filepath.Dir(filepath.Dir(home))
	}
	installation, err := toolchain.ProbeJava(ctx, home)
	if err != nil {
		return err
	}
	if relative, err := filepath.Rel(root, home); err == nil &&
		relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		home = relative
	}
	local.Java = &manifest.LocalJava{Home: home}
	log.Info("local java selected", "home", home, "version", installation.Version)
	return manifest.WriteLocal(root, local)
}
