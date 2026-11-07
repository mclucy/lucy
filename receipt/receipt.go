// Package receipt owns the workspace installation receipt: the authoritative
// ledger of committed runtime outputs and the resolutions that produced them.
package receipt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"go.yaml.in/yaml/v4"
)

const (
	// Filename is the receipt's path inside a workspace.
	Filename = ".lucy/install.yaml"
	// Version is the schema version this build reads and writes.
	Version = 1

	// OriginBootstrap identifies outputs produced by server bootstrap.
	OriginBootstrap = "bootstrap"
	// OriginPackage identifies outputs installed from packages.
	OriginPackage = "package"
	// OriginMCDR identifies outputs produced by the MCDReforged wrapper
	// runtime, which is reconciled independently of the server bootstrap.
	OriginMCDR = "mcdr"
)

// ErrLegacyReceipt is returned when a receipt was written by an older version of Lucy
// that lacks provenance origin attribution.
type ErrLegacyReceipt struct {
	Receipt *Receipt
}

func (e *ErrLegacyReceipt) Error() string {
	return fmt.Sprintf("legacy installation receipt in %s requires explicit adoption; run 'lucy init' (or 'lucy init --force') to adopt existing files safely", Filename)
}

// Receipt records the outputs an installation committed. It is an authoritative
// local ledger of committed paths Lucy owns, their origins, and update policies.
// It is not a second lock: paths and digests describe local state, and the
// fingerprints name the resolution that produced it.
type Receipt struct {
	Version              int     `yaml:"version"`
	BootstrapFingerprint string  `yaml:"bootstrap_fingerprint,omitempty"`
	PackageFingerprint   string  `yaml:"package_fingerprint,omitempty"`
	LockFingerprint      string  `yaml:"lock_fingerprint,omitempty"`
	Entries              []Entry `yaml:"entries"`
}

// Entry records one committed managed output.
type Entry struct {
	Path    string `yaml:"path"`
	SHA256  string `yaml:"sha256"`
	Mode    uint32 `yaml:"mode"`
	Origin  string `yaml:"origin"`
	Package string `yaml:"package,omitempty"`
}

// New returns an empty receipt for the current schema.
func New() Receipt {
	return Receipt{Version: Version, Entries: []Entry{}}
}

// Validate verifies receipt structural invariants: valid schema version,
// safe relative clean paths, origin attribution, valid SHA256 hex digests,
// non-reserved paths, and absence of duplicate or colliding entries.
// Ambiguous legacy receipts (such as those with lock_fingerprint or missing
// origins) return ErrLegacyReceipt.
func (r *Receipt) Validate() error {
	if r.Version != Version {
		return fmt.Errorf("unsupported receipt version %d", r.Version)
	}
	isLegacy := r.LockFingerprint != ""
	seen := make(map[string]string, len(r.Entries))
	for i := range r.Entries {
		entry := &r.Entries[i]
		if entry.Path == "" {
			return errors.New("receipt entry has empty path")
		}
		switch entry.Origin {
		case OriginBootstrap, OriginMCDR:
			if entry.Package != "" {
				return fmt.Errorf("receipt entry %s has origin %s but non-empty package %q", entry.Path, entry.Origin, entry.Package)
			}
		case OriginPackage:
			if entry.Package == "" {
				return fmt.Errorf("receipt entry %s has origin %s but empty package identity", entry.Path, OriginPackage)
			}
		case "":
			isLegacy = true
		default:
			return fmt.Errorf("receipt entry %s has unsupported origin %q; expected %q, %q or %q", entry.Path, entry.Origin, OriginBootstrap, OriginPackage, OriginMCDR)
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(entry.Path)))
		if clean != entry.Path || filepath.IsAbs(entry.Path) || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("unsafe or unnormalized receipt path: %s", entry.Path)
		}
		if err := document.SafeRelativePath(clean); err != nil {
			return fmt.Errorf("unsafe receipt path %s: %w", entry.Path, err)
		}
		lower := strings.ToLower(clean)
		if lower == ".lucy" || strings.HasPrefix(lower, ".lucy/") || lower == strings.ToLower(document.LockFilename) || lower == strings.ToLower(document.ManifestFilename) {
			return fmt.Errorf("receipt path %s is reserved", entry.Path)
		}
		if len(entry.SHA256) != 64 {
			return fmt.Errorf("receipt entry %s has invalid sha256 digest %q", entry.Path, entry.SHA256)
		}
		for _, c := range entry.SHA256 {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return fmt.Errorf("receipt entry %s has non-hex sha256 digest %q", entry.Path, entry.SHA256)
			}
		}
		r.Entries[i].SHA256 = strings.ToLower(entry.SHA256)
		if prior, exists := seen[lower]; exists {
			return fmt.Errorf("receipt path collision: %s and %s", prior, entry.Path)
		}
		seen[lower] = clean
	}
	if isLegacy {
		return &ErrLegacyReceipt{Receipt: r}
	}
	return nil
}

// Decode parses receipt bytes strictly and verifies schema version and entry validity.
// If the receipt contains legacy fields or entries lacking origin attribution,
// Decode returns the parsed receipt alongside an *ErrLegacyReceipt error so adoption
// callers can preserve baselines.
func Decode(reader io.Reader) (*Receipt, error) {
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var result Receipt
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode receipt YAML: %w", err)
	}
	for i := range result.Entries {
		result.Entries[i].SHA256 = strings.ToLower(result.Entries[i].SHA256)
	}
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return nil, errors.New("receipt contains unexpected trailing documents")
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("check trailing receipt documents: %w", err)
	}
	if err := result.Validate(); err != nil {
		var legacyErr *ErrLegacyReceipt
		if errors.As(err, &legacyErr) {
			return &result, err
		}
		return nil, err
	}
	return &result, nil
}

// ReadAt reads the receipt at path. A missing file returns an error that
// matches os.ErrNotExist.
func ReadAt(path string) (*Receipt, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refuse symlinked or non-regular receipt %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(bytes.NewReader(raw))
}

// Read reads the receipt of the workspace at root after validating confinement.
func Read(root string) (*Receipt, error) {
	if err := workspacewrite.SafePath(root, Filename); err != nil {
		return nil, err
	}
	return ReadAt(filepath.Join(root, Filename))
}

// Snapshot returns the receipt bytes without writing them.
func (r *Receipt) Snapshot() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	result := *r
	result.LockFingerprint = ""
	if result.Entries == nil {
		result.Entries = []Entry{}
	} else {
		result.Entries = slices.Clone(r.Entries)
		slices.SortFunc(result.Entries, func(a, b Entry) int {
			return strings.Compare(a.Path, b.Path)
		})
	}
	return yaml.Marshal(result)
}
