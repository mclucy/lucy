package state

import (
	"encoding/json"
	"fmt"
	"time"
)

// Lock represents Lucy's exact resolved state snapshot.
// It is persisted in .lucy/lock.json and owns resolution.graph,
// resolution.provenance, artifact.hashes, and artifact.download-urls.
//
// Lock MUST NOT own policy defaults, desired roots, or observed runtime state.
type Lock struct {
	Version             string          `json:"version"`
	GeneratedAt         string          `json:"generated_at"`
	ManifestFingerprint string          `json:"manifest_fingerprint"`
	GameVersion         string          `json:"game_version"`
	Platform            string          `json:"platform"`
	PlatformVersion     string          `json:"platform_version"`
	Packages            []LockedPackage `json:"packages"`
	Bundles             []LockedBundle  `json:"bundles"`
}

// LockedPackage records one exact resolved artifact and how it entered the
// resolved graph.
type LockedPackage struct {
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	Source        string   `json:"source"`
	URL           string   `json:"url"`
	Filename      string   `json:"filename"`
	Hash          string   `json:"hash"`
	HashAlgorithm string   `json:"hash_algorithm"`
	InstallPath   string   `json:"install_path"`
	Side          string   `json:"side"`
	Optional      bool     `json:"optional"`
	Embedded      bool     `json:"embedded"`
	EmbeddedIn    string   `json:"embedded_in,omitempty"`
	Provenance    []string `json:"provenance,omitempty"`
	Requester     string   `json:"requester"`
}

// LockedBundle records one non-package managed artifact bundle tracked in the
// resolved state.
type LockedBundle struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Hash        string `json:"hash"`
	InstallPath string `json:"install_path"`
}

// NewLock returns a new v1 lock with the current timestamp in RFC3339 format.
func NewLock() Lock {
	return Lock{
		Version:     "v1",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Packages:    []LockedPackage{},
		Bundles:     []LockedBundle{},
	}
}

// ValidateLock validates required fields and v1 resolved-state invariants.
func ValidateLock(l Lock) error {
	if l.Version == "" {
		return fmt.Errorf("lock: version is required")
	}
	if l.Version != "v1" {
		return fmt.Errorf("lock: unsupported version %q", l.Version)
	}
	if l.GeneratedAt == "" {
		return fmt.Errorf("lock: generated_at is required")
	}
	if _, err := time.Parse(time.RFC3339, l.GeneratedAt); err != nil {
		return fmt.Errorf("lock: generated_at must be RFC3339: %w", err)
	}
	if l.ManifestFingerprint == "" {
		return fmt.Errorf("lock: manifest_fingerprint is required")
	}
	if l.GameVersion == "" {
		return fmt.Errorf("lock: game_version is required")
	}
	if l.Platform == "" {
		return fmt.Errorf("lock: platform is required")
	}
	if l.PlatformVersion == "" {
		return fmt.Errorf("lock: platform_version is required")
	}

	for i, pkg := range l.Packages {
		if err := validateLockedPackage(pkg); err != nil {
			return fmt.Errorf("lock: packages[%d]: %w", i, err)
		}
	}

	for i, bundle := range l.Bundles {
		if err := validateLockedBundle(bundle); err != nil {
			return fmt.Errorf("lock: bundles[%d]: %w", i, err)
		}
	}

	return nil
}

func (l Lock) Marshal() ([]byte, error) {
	return json.MarshalIndent(l, "", "  ")
}

func (l *Lock) Unmarshal(data []byte) error {
	return json.Unmarshal(data, l)
}

func validateLockedPackage(pkg LockedPackage) error {
	if pkg.ID == "" {
		return fmt.Errorf("id is required")
	}
	if pkg.Version == "" {
		return fmt.Errorf("version is required")
	}
	if isSpecialLockVersion(pkg.Version) {
		return fmt.Errorf("version must be exact, got %q", pkg.Version)
	}
	if !isValidLockSource(pkg.Source) {
		return fmt.Errorf("invalid source %q", pkg.Source)
	}
	if pkg.URL == "" {
		return fmt.Errorf("url is required")
	}
	if pkg.Filename == "" {
		return fmt.Errorf("filename is required")
	}
	if pkg.Hash == "" {
		return fmt.Errorf("hash is required")
	}
	if !isValidHashAlgorithm(pkg.HashAlgorithm) {
		return fmt.Errorf("invalid hash_algorithm %q", pkg.HashAlgorithm)
	}
	if pkg.InstallPath == "" {
		return fmt.Errorf("install_path is required")
	}
	if !isValidPackageSide(pkg.Side) {
		return fmt.Errorf("invalid side %q", pkg.Side)
	}
	if len(pkg.Provenance) == 0 {
		return fmt.Errorf("provenance is required")
	}
	for i, step := range pkg.Provenance {
		if step == "" {
			return fmt.Errorf("provenance[%d] is required", i)
		}
	}
	if pkg.Requester == "" {
		return fmt.Errorf("requester is required")
	}
	if pkg.Embedded {
		if pkg.EmbeddedIn == "" {
			return fmt.Errorf("embedded_in is required when embedded is true")
		}
	} else if pkg.EmbeddedIn != "" {
		return fmt.Errorf("embedded_in must be empty when embedded is false")
	}
	return nil
}

func validateLockedBundle(bundle LockedBundle) error {
	if bundle.Name == "" {
		return fmt.Errorf("name is required")
	}
	if bundle.Type == "" {
		return fmt.Errorf("type is required")
	}
	if bundle.Hash == "" {
		return fmt.Errorf("hash is required")
	}
	if bundle.InstallPath == "" {
		return fmt.Errorf("install_path is required")
	}
	return nil
}

func isSpecialLockVersion(version string) bool {
	switch version {
	case "any", "none", "unknown", "latest", "compatible":
		return true
	default:
		return false
	}
}

func isValidLockSource(source string) bool {
	switch source {
	case "modrinth", "curseforge", "github", "mcdr", "direct":
		return true
	default:
		return false
	}
}

func isValidHashAlgorithm(algorithm string) bool {
	switch algorithm {
	case "sha512", "sha1":
		return true
	default:
		return false
	}
}

func isValidPackageSide(side string) bool {
	switch side {
	case "server", "client", "both":
		return true
	default:
		return false
	}
}
