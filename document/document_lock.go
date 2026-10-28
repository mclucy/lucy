package document

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"slices"

	"github.com/mclucy/lucy/catalog"
	javapkg "github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
	"gopkg.in/yaml.v3"
)

const (
	LockFilename      = "lucy-lock.yaml"
	LockSchemaVersion = 1
)

type Lock struct {
	SchemaVersion       int           `yaml:"lock_version" json:"lock_version"`
	ManifestFingerprint string        `yaml:"manifest_fingerprint" json:"manifest_fingerprint"`
	Server              LockedServer  `yaml:"server" json:"server"`
	MCDR                *LockedMCDR   `yaml:"mcdr,omitempty" json:"mcdr,omitempty"`
	Packages            []Package     `yaml:"packages" json:"packages"`
	Files               []TrackedFile `yaml:"files,omitempty" json:"files,omitempty"`
}

type LockedServer struct {
	Directory        string          `yaml:"directory,omitempty" json:"directory"`
	Minecraft        version.Version `yaml:"minecraft" json:"minecraft"`
	Product          server.Product  `yaml:"product" json:"product"`
	CoreVersion      version.Version `yaml:"core_version" json:"core_version"`
	catalog.Artifact `yaml:",inline" json:",inline"`
	Bootstrapper     string                  `yaml:"bootstrapper" json:"bootstrapper"`
	Inputs           []server.BootstrapInput `yaml:"inputs,omitempty" json:"inputs"`
	Java             LockedJava              `yaml:"java" json:"java"`
}

// LockedJava records the inputs of the Java requirement. Package requirements
// on Java stay in the package modules, so commands combine the inputs when
// they check a Java runtime.
type LockedJava struct {
	// Advisory is the Java major version that Mojang recommends for the
	// Minecraft version. Lucy treats it as a minimum.
	Advisory int `yaml:"advisory" json:"advisory"`
	// Version is the major version that the manifest pins; zero follows the
	// advisory.
	Version int `yaml:"version,omitempty" json:"version,omitempty"`
	// Distribution is the distribution that the manifest pins; empty accepts
	// any build.
	Distribution javapkg.Distribution `yaml:"distribution,omitempty" json:"distribution,omitempty"`
}

// NewLockedJava records the Mojang advisory with the override that the
// manifest declares.
func NewLockedJava(advisory int, override *ManifestJava) LockedJava {
	java := LockedJava{Advisory: advisory}
	if override != nil {
		java.Version = override.Version
		java.Distribution = override.Distribution
	}
	return java
}

type LockedMCDR struct {
	Directory string          `yaml:"directory" json:"directory"`
	Version   version.Version `yaml:"version" json:"version"`
}

func (l *Lock) Validate() error {
	if l.SchemaVersion != LockSchemaVersion {
		return fmt.Errorf("unsupported lock version %d", l.SchemaVersion)
	}
	if l.ManifestFingerprint == "" {
		return fmt.Errorf("manifest_fingerprint is required")
	}
	if l.Server.Directory == "" {
		return fmt.Errorf("server.directory is required")
	}
	if err := SafeRelativePath(l.Server.Directory); err != nil {
		return err
	}
	if err := validateVersion("server.minecraft", l.Server.Minecraft, false); err != nil {
		return err
	}
	if l.Server.Product == server.ProductUnknown {
		return fmt.Errorf("server.product is required")
	}
	if err := validateVersion("server.core_version", l.Server.CoreVersion, true); err != nil {
		return err
	}
	if l.Server.Artifact.Version == "" {
		return fmt.Errorf("server.artifact.version is required")
	}
	if l.Server.Artifact.Filename == "" {
		return fmt.Errorf("server.artifact.filename is required")
	}
	if l.Server.Java.Advisory <= 0 {
		return fmt.Errorf("server.java.advisory must be positive")
	}
	distribution, ok := javapkg.ParseDistribution(string(l.Server.Java.Distribution))
	if !ok {
		return fmt.Errorf("unknown server.java.distribution %q", l.Server.Java.Distribution)
	}
	l.Server.Java.Distribution = distribution
	if l.MCDR != nil {
		if l.MCDR.Directory == "" {
			return fmt.Errorf("mcdr.directory is required")
		}
		if err := SafeRelativePath(l.MCDR.Directory); err != nil {
			return err
		}
		if err := validateVersion("mcdr.version", l.MCDR.Version, false); err != nil {
			return err
		}
	}
	for i := range l.Packages {
		if err := validateLockedPackage(&l.Packages[i]); err != nil {
			return err
		}
	}
	if err := NormalizeTrackedFiles(l.Files); err != nil {
		return err
	}
	return nil
}

func validateVersion(field string, v version.Version, allowSelector bool) error {
	if v == "" {
		return fmt.Errorf("%s is required", field)
	}
	if !allowSelector && (v.CanInfer() || v.IsInvalid()) {
		return fmt.Errorf("%s must be a concrete version", field)
	}
	return nil
}

func validateLockedPackage(pkg *Package) error {
	switch pkg.Environment {
	case server.EnvironmentServer, server.EnvironmentMCDR:
	default:
		return fmt.Errorf("package %s has unknown environment %q", pkg.Artifact.ProjectID, pkg.Environment)
	}
	if !pkg.Ecosystem.Valid() || pkg.Ecosystem == catalog.EcosystemUnspecified {
		return fmt.Errorf("package %s has unknown ecosystem %q", pkg.Artifact.ProjectID, pkg.Ecosystem)
	}
	if pkg.Artifact.Provider == catalog.ProviderUnspecified || pkg.Artifact.Provider == catalog.ProviderUnknown {
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
	for j := range pkg.Modules {
		module := &pkg.Modules[j]
		if !module.VersionDialect.Valid() {
			return fmt.Errorf(
				"package %s module %s requires a known version dialect, got %q",
				pkg.Artifact.ProjectID,
				module.ID,
				module.VersionDialect,
			)
		}
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

// DecodeLock validates lock bytes from a reader.
func DecodeLock(reader io.Reader) (*Lock, error) {
	l := &Lock{}
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(l); err != nil {
		return nil, fmt.Errorf("decode lock: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("lock must contain one YAML document")
		}
		return nil, fmt.Errorf("decode lock tail: %w", err)
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return l, nil
}

// Fingerprint returns the identity of a lock's content. It validates a copy
// first, so the fingerprint describes what the lock says and not how it was
// spelled.
func (l *Lock) Fingerprint() (string, error) {
	canonical, err := l.canonical()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(canonical, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (l *Lock) canonical() (*Lock, error) {
	if l == nil {
		return nil, nil
	}
	clone := *l
	clone.Files = slices.Clone(l.Files)
	if err := clone.Validate(); err != nil {
		return nil, err
	}
	return &clone, nil
}

// Snapshot returns the canonical bytes of the document. It writes nothing. A
// publisher that decides when and where the bytes land takes a snapshot first;
// Service.Write takes one and publishes it.
func (l *Lock) Snapshot() ([]byte, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return NewCommenter(YAMLOptions{NoComments: true}).Render(l)
}

// MergeBytes writes l into existing and keeps the comments the existing lock
// carries. The lock declares no generated comments of its own.
func (l *Lock) MergeBytes(existing []byte) ([]byte, error) {
	if len(bytes.TrimSpace(existing)) == 0 {
		return l.Snapshot()
	}
	if _, err := DecodeLock(bytes.NewReader(existing)); err != nil {
		return nil, err
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return NewCommenter(YAMLOptions{NoComments: true}).Merge(existing, l)
}

// LookupReference expands a bare name against the package names in this lock.
// It returns an empty string when no package matches and an error when the
// name is ambiguous.
func (l *Lock) LookupReference(input string) (string, error) {
	var match string
	for _, pkg := range l.Packages {
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
