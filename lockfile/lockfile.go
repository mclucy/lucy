package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

const (
	Filename      = "lucy-lock.yaml"
	SchemaVersion = 1
)

type Document struct {
	SchemaVersion       int                   `yaml:"lock_version" json:"lock_version"`
	ManifestFingerprint string                `yaml:"manifest_fingerprint" json:"manifest_fingerprint"`
	Server              Server                `yaml:"server" json:"server"`
	MCDR                *MCDR                 `yaml:"mcdr,omitempty" json:"mcdr,omitempty"`
	Packages            []types.LockedPackage `yaml:"packages" json:"packages"`
	Files               []types.TrackedFile   `yaml:"files,omitempty" json:"files,omitempty"`
}

type Server struct {
	Directory      string              `yaml:"directory,omitempty" json:"directory"`
	Minecraft      types.Version       `yaml:"minecraft" json:"minecraft"`
	Product        types.ServerProduct `yaml:"product" json:"product"`
	CoreVersion    types.Version       `yaml:"core_version" json:"core_version"`
	types.Artifact `yaml:",inline" json:",inline"`
	Bootstrapper   string                 `yaml:"bootstrapper" json:"bootstrapper"`
	Inputs         []types.BootstrapInput `yaml:"inputs,omitempty" json:"inputs"`
	Java           int                    `yaml:"java" json:"java"`
}

type MCDR struct {
	Directory string                 `yaml:"directory" json:"directory"`
	Version   types.Version          `yaml:"version" json:"version"`
	Python    string                 `yaml:"python" json:"python"`
	Target    string                 `yaml:"target" json:"target"`
	Inputs    []types.BootstrapInput `yaml:"inputs,omitempty" json:"inputs"`
}

func (d *Document) Validate() error {
	if d.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported lock version %d", d.SchemaVersion)
	}
	if d.ManifestFingerprint == "" {
		return fmt.Errorf("manifest_fingerprint is required")
	}
	if d.Server.Directory == "" {
		return fmt.Errorf("server.directory is required")
	}
	if err := manifest.SafeRelativePath(d.Server.Directory); err != nil {
		return err
	}
	if err := validateVersion("server.minecraft", d.Server.Minecraft, false); err != nil {
		return err
	}
	if d.Server.Product == types.ServerProductUnknown {
		return fmt.Errorf("server.product is required")
	}
	if err := validateVersion("server.core_version", d.Server.CoreVersion, true); err != nil {
		return err
	}
	if d.Server.Artifact.Version == "" {
		return fmt.Errorf("server.artifact.version is required")
	}
	if d.Server.Artifact.Filename == "" {
		return fmt.Errorf("server.artifact.filename is required")
	}
	if d.Server.Java <= 0 {
		return fmt.Errorf("server.java must be positive")
	}
	if d.MCDR != nil {
		if d.MCDR.Directory == "" {
			return fmt.Errorf("mcdr.directory is required")
		}
		if err := manifest.SafeRelativePath(d.MCDR.Directory); err != nil {
			return err
		}
		if err := validateVersion("mcdr.version", d.MCDR.Version, false); err != nil {
			return err
		}
		if d.MCDR.Python == "" {
			return fmt.Errorf("mcdr.python is required")
		}
		if d.MCDR.Target == "" {
			return fmt.Errorf("mcdr.target is required")
		}
	}
	for i := range d.Packages {
		if err := validateLockedPackage(&d.Packages[i]); err != nil {
			return err
		}
	}
	if err := input.NormalizeTrackedFiles(d.Files); err != nil {
		return err
	}
	return nil
}

func validateVersion(field string, v types.Version, allowSelector bool) error {
	if v == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !allowSelector && (v.CanInfer() || v.IsInvalid()) {
		return fmt.Errorf("%s must be a concrete version", field)
	}
	return nil
}

func validateLockedPackage(pkg *types.LockedPackage) error {
	switch pkg.Environment {
	case types.EnvironmentServer, types.EnvironmentMCDR:
	default:
		return fmt.Errorf("package %s has unknown environment %q", pkg.Artifact.ProjectID, pkg.Environment)
	}
	if !pkg.Ecosystem.Valid() || pkg.Ecosystem == types.EcosystemUnspecified {
		return fmt.Errorf("package %s has unknown ecosystem %q", pkg.Artifact.ProjectID, pkg.Ecosystem)
	}
	if pkg.Artifact.Provider == types.ProviderUnspecified || pkg.Artifact.Provider == types.ProviderUnknown {
		return fmt.Errorf("package %s has no provider", pkg.Artifact.ProjectID)
	}
	if pkg.Artifact.Version == "" || pkg.Artifact.Version.CanInfer() || pkg.Artifact.Version.IsInvalid() {
		return fmt.Errorf("package %s has no concrete version", pkg.Artifact.ProjectID)
	}
	if pkg.Artifact.Filename == "" {
		return fmt.Errorf("package %s has no filename", pkg.Artifact.ProjectID)
	}
	if pkg.Artifact.Hashes["sha256"] == "" {
		return fmt.Errorf("package %s has no sha256 hash", pkg.Artifact.ProjectID)
	}
	for j := range pkg.Requirements {
		requirement := &pkg.Requirements[j]
		if requirement.Reference == "" {
			return fmt.Errorf("package %s has an empty requirement reference", pkg.Artifact.ProjectID)
		}
		if requirement.Selector == "" {
			return fmt.Errorf("package %s has an empty requirement selector", pkg.Artifact.ProjectID)
		}
	}
	return nil
}

func Read(root string) (*Document, error) {
	file, err := os.Open(filepath.Join(root, Filename))
	if err != nil {
		return nil, err
	}
	d, decodeErr := Decode(file)
	return d, errors.Join(decodeErr, file.Close())
}

// Decode validates lock bytes from a reader.
func Decode(reader io.Reader) (*Document, error) {
	d := &Document{}
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(d); err != nil {
		return nil, fmt.Errorf("decode lock: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("lock must contain one YAML document")
		}
		return nil, fmt.Errorf("decode lock tail: %w", err)
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
}

func Fingerprint(d *Document) (string, error) {
	d = cloneForFingerprint(d)
	data, err := json.Marshal(d, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func cloneForFingerprint(d *Document) *Document {
	if d == nil {
		return nil
	}
	clone := *d
	clone.ManifestFingerprint = d.ManifestFingerprint
	return &clone
}

func Write(root string, d *Document) error {
	if err := d.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(d)
	if err != nil {
		return err
	}
	path := filepath.Join(root, Filename)
	if err := fsutil.AtomicWrite(path, data, 0o644); err != nil {
		return err
	}
	log.Info("lock written", "path", path)
	return nil
}

// LookupReference expands a bare name against the package names in this lock.
// It returns an empty string when no package matches and an error when the
// name is ambiguous.
func (d *Document) LookupReference(input string) (string, error) {
	var match string
	for _, pkg := range d.Packages {
		if pkg.Artifact.ProjectID != input {
			continue
		}
		ref := pkg.Artifact.Provider.String() + ":" + pkg.Artifact.ProjectID
		if match != "" && match != ref {
			return "", fmt.Errorf("ambiguous package name %q", input)
		}
		match = ref
	}
	return match, nil
}
