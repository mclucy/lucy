package state

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mclucy/lucy/types"
)

// Lock represents Lucy's exact resolved state snapshot.
// It is persisted in lucy-lock.yaml.
type Lock struct {
	Version     string `yaml:"version"`
	GeneratedAt string `yaml:"generated_at"`
	// ManifestFingerprint binds the exact lock facts to one serialized manifest
	// intent document. If the manifest bytes change, the lock is stale even when
	// package IDs still overlap.
	ManifestFingerprint string          `yaml:"manifest_fingerprint"`
	GameVersion         string          `yaml:"game_version"`
	Platform            string          `yaml:"platform"`
	PlatformVersion     string          `yaml:"platform_version"`
	Packages            []LockedPackage `yaml:"packages"`
	Bundles             []LockedBundle  `yaml:"bundles"`
}

// LockedPackage records one exact resolved artifact and how it entered the
// resolved graph.
type LockedPackage struct {
	ID string `yaml:"id"`
	// Version is the final concrete version chosen for this resolved artifact.
	// Lock entries are fact records, so fuzzy selectors and ranges are invalid
	// here even when the manifest used them as intent.
	Version       string   `yaml:"version"`
	Source        string   `yaml:"source"`
	URL           string   `yaml:"url"`
	Filename      string   `yaml:"filename"`
	Hash          string   `yaml:"hash"`
	HashAlgorithm string   `yaml:"hash_algorithm"`
	InstallPath   string   `yaml:"install_path"`
	Side          string   `yaml:"side"`
	Optional      bool     `yaml:"optional"`
	Embedded      bool     `yaml:"embedded"`
	EmbeddedIn    string   `yaml:"embedded_in,omitempty"`
	Provenance    []string `yaml:"provenance,omitempty"`
	Requester     string   `yaml:"requester"`
}

// LockedBundle records one non-package managed artifact bundle tracked in the
// resolved state.
type LockedBundle struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Hash        string `yaml:"hash"`
	InstallPath string `yaml:"install_path"`
}

// NewLock returns a new v1 lock with the current timestamp in RFC3339 format.
func NewLock() Lock {
	return Lock{
		Version:     SupportedVersion,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Packages:    []LockedPackage{},
		Bundles:     []LockedBundle{},
	}
}

// ValidateLock validates required fields and v1 resolved-state invariants.
func ValidateLock(l Lock) error {
	if err := ValidateVersion(l.Version); err != nil {
		if IsVersionError(err) {
			return versionStateError(
				LockFile,
				"version",
				l.Version,
				ErrVersionUnsupported,
			)
		}
		return versionStateError(LockFile, "version", l.Version, ErrMalformed)
	}
	if l.GeneratedAt == "" {
		return NewStateError(
			LockFile,
			ErrMalformed,
			"generated_at",
			"generated_at is required",
		)
	}
	if _, err := time.Parse(time.RFC3339, l.GeneratedAt); err != nil {
		return NewStateError(
			LockFile,
			ErrMalformed,
			"generated_at",
			fmt.Sprintf("generated_at must be RFC3339: %v", err),
		)
	}
	if l.ManifestFingerprint == "" {
		return NewStateError(
			LockFile,
			ErrMalformed,
			"manifest_fingerprint",
			"manifest_fingerprint is required",
		)
	}
	if l.GameVersion == "" {
		return NewStateError(
			LockFile,
			ErrMalformed,
			"game_version",
			"game_version is required",
		)
	}
	if l.Platform == "" {
		return NewStateError(
			LockFile,
			ErrMalformed,
			"platform",
			"platform is required",
		)
	}
	if err := validateManifestPlatform(l.Platform); err != nil {
		return NewStateError(LockFile, ErrMalformed, "platform", err.Error())
	}
	if l.PlatformVersion == "" {
		return NewStateError(
			LockFile,
			ErrMalformed,
			"platform_version",
			"platform_version is required",
		)
	}

	for i, pkg := range l.Packages {
		if err := validateLockedPackage(pkg); err != nil {
			return malformedStateError(
				LockFile,
				fmt.Sprintf("packages[%d]", i),
				err,
			)
		}
	}

	for i, bundle := range l.Bundles {
		if err := validateLockedBundle(bundle); err != nil {
			return malformedStateError(
				LockFile,
				fmt.Sprintf("bundles[%d]", i),
				err,
			)
		}
	}

	return nil
}

func (l Lock) Marshal() ([]byte, error) {
	return yaml.Marshal(l)
}

func (l *Lock) Unmarshal(data []byte) error {
	return yaml.Unmarshal(data, l)
}

func validateLockedPackage(pkg LockedPackage) error {
	if pkg.ID == "" {
		return fmt.Errorf("id is required")
	}
	parts := strings.Split(pkg.ID, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("id must use platform/name format")
	}
	platform := types.PlatformId(parts[0])
	if !platform.Valid() || platform == types.PlatformAny || platform == types.PlatformMinecraft || platform == types.PlatformUnknown {
		return fmt.Errorf("invalid package platform %q", parts[0])
	}
	if pkg.Version == "" {
		return fmt.Errorf("version is required")
	}
	if !isExactLockVersion(pkg.Version) {
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

func isExactLockVersion(version string) bool {
	if isSpecialLockVersion(version) {
		return false
	}
	for _, token := range []string{
		" ", "\t", "\n", "\r", ",", "||", "*", "^", "~", ">", "<", "=", "[",
		"]", "(", ")",
	} {
		if strings.Contains(version, token) {
			return false
		}
	}
	for _, token := range []string{".x", ".X", "-x", "-X", "x.", "X."} {
		if strings.Contains(version, token) {
			return false
		}
	}
	if strings.EqualFold(version, "x") {
		return false
	}
	return true
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

func CanonicalLockedPackages(packages []LockedPackage) []LockedPackage {
	canonical := append([]LockedPackage(nil), packages...)
	sort.Slice(
		canonical, func(i, j int) bool {
			if canonical[i].ID != canonical[j].ID {
				return canonical[i].ID < canonical[j].ID
			}
			if canonical[i].Version != canonical[j].Version {
				return canonical[i].Version < canonical[j].Version
			}
			return canonical[i].InstallPath < canonical[j].InstallPath
		},
	)
	return canonical
}

func PruneLockForManifest(lock *Lock, manifest *Manifest) *Lock {
	if lock == nil {
		return nil
	}
	pruned := *lock
	pruned.Bundles = append([]LockedBundle(nil), lock.Bundles...)
	allowed := make(map[string]struct{})
	if manifest != nil {
		for _, pkg := range manifest.Packages {
			if pkg.Role == RoleIgnored {
				continue
			}
			allowed[pkg.ID] = struct{}{}
		}
	}
	packages := make([]LockedPackage, 0, len(lock.Packages))
	for _, pkg := range lock.Packages {
		if _, ok := allowed[pkg.ID]; ok {
			packages = append(packages, pkg)
		}
	}
	pruned.Packages = CanonicalLockedPackages(packages)
	return &pruned
}
