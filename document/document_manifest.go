package document

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
	"gopkg.in/yaml.v3"
)

const (
	ManifestFilename      = "lucy.yaml"
	ManifestSchemaVersion = 1
)

type Manifest struct {
	SchemaVersion int            `yaml:"manifest_version" json:"manifest_version"`
	Server        ManifestServer `yaml:"server" json:"server"`
	MCDR          *ManifestMCDR  `yaml:"mcdr,omitempty" json:"mcdr,omitempty"`
	Files         []TrackedFile  `yaml:"files,omitempty" json:"files,omitempty"`
}

type ManifestCore struct {
	Product        server.Product  `yaml:"product" json:"product"`
	ProductVersion version.Version `yaml:"product_version,omitempty" json:"product_version"`
	Repository     string          `yaml:"repository,omitempty" json:"repository,omitempty"`
	FilePattern    string          `yaml:"file_pattern,omitempty" json:"file_pattern,omitempty"`
}

type ManifestServer struct {
	Minecraft version.Version                              `yaml:"minecraft" json:"minecraft"`
	Directory string                                       `yaml:"directory,omitempty" json:"directory"`
	Core      ManifestCore                                 `yaml:"core" json:"core"`
	Java      *ManifestJava                                `yaml:"java,omitempty" json:"java,omitempty"`
	Packages  map[catalog.Ecosystem]map[string]Requirement `yaml:"packages" json:"packages"`
}

type ManifestMCDR struct {
	Directory string                 `yaml:"directory,omitempty" json:"directory"`
	Version   version.Version        `yaml:"version,omitempty" json:"version"`
	Packages  map[string]Requirement `yaml:"packages" json:"packages"`
}

type Requirement struct {
	Selector    version.Version `yaml:"selector,omitempty" json:"selector"`
	FilePattern string          `yaml:"file_pattern,omitempty" json:"file_pattern,omitempty"`
	Enabled     *bool           `yaml:"enabled,omitempty" json:"enabled"`
}

func (r Requirement) IsEnabled() bool { return r.Enabled == nil || *r.Enabled }

func (r *Requirement) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		if n.Tag != "!!str" {
			return fmt.Errorf("package selector must be a quoted string")
		}
		r.Selector = version.Version(n.Value)
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("package requirement must be a string or object")
	}
	for i := 0; i < len(n.Content); i += 2 {
		switch n.Content[i].Value {
		case "selector", "file_pattern", "enabled":
		default:
			return fmt.Errorf("unknown package requirement field %q", n.Content[i].Value)
		}
	}
	type plain Requirement
	return n.Decode((*plain)(r))
}

func (r Requirement) MarshalYAML() (any, error) {
	if r.FilePattern == "" && r.IsEnabled() {
		return r.Selector.String(), nil
	}
	type plain Requirement
	return plain(r), nil
}

// ParseReference splits a versionless provider:project reference.
func ParseReference(ref string) (provider catalog.Provider, project string, err error) {
	providerKey, project, ok := strings.Cut(ref, ":")
	if !ok || project == "" || strings.Contains(ref, "@") {
		return catalog.ProviderUnknown, "", fmt.Errorf("require provider:project reference, got %q", ref)
	}
	provider, ok = catalog.ParseProvider(providerKey)
	if !ok || !isPackageProvider(provider) {
		return catalog.ProviderUnknown, "", fmt.Errorf("unsupported package provider %q", providerKey)
	}
	return provider, project, nil
}

func isPackageProvider(provider catalog.Provider) bool {
	switch provider {
	case catalog.ProviderModrinth, catalog.ProviderCurseForge, catalog.ProviderMCDR:
		return true
	default:
		return false
	}
}

// SafeRelativePath reports whether path is a workspace-relative path that
// stays inside the workspace.
func SafeRelativePath(path string) error {
	if path == "" || filepath.IsAbs(path) {
		return fmt.Errorf("path must be workspace-relative: %q", path)
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes workspace: %q", path)
	}
	return nil
}

func (m *Manifest) Normalize() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return fmt.Errorf("unsupported manifest version %d", m.SchemaVersion)
	}
	m.Server.Minecraft = version.Version(strings.TrimSpace(m.Server.Minecraft.String()))
	if declared := m.Server.Minecraft; declared == "" || declared.CanInfer() || declared.IsInvalid() {
		return fmt.Errorf("server.minecraft must specify a concrete Minecraft version")
	}
	m.Server.Core.Product = server.Product(strings.ToLower(strings.TrimSpace(m.Server.Core.Product.String())))
	if m.Server.Core.Product == server.ProductUnknown {
		return fmt.Errorf("server core product is required")
	}
	if product, known := server.ParseProduct(m.Server.Core.Product.String()); known {
		m.Server.Core.Product = product
	} else if m.Server.Core.Repository == "" {
		return fmt.Errorf("unsupported server product %q; GitHub forks require core.repository", m.Server.Core.Product)
	}
	if m.Server.Core.ProductVersion == "" {
		m.Server.Core.ProductVersion = version.Any
	}
	if m.Server.Directory == "" {
		m.Server.Directory = "."
	}
	if err := SafeRelativePath(m.Server.Directory); err != nil {
		return err
	}
	m.Server.Directory = filepath.ToSlash(filepath.Clean(m.Server.Directory))
	if err := normalizeJava(&m.Server); err != nil {
		return err
	}
	if m.Server.Packages == nil {
		m.Server.Packages = map[catalog.Ecosystem]map[string]Requirement{}
	}
	for ecosystem, packages := range m.Server.Packages {
		switch ecosystem {
		case catalog.EcosystemFabric,
			catalog.EcosystemForge,
			catalog.EcosystemNeoForge,
			catalog.EcosystemBukkit,
			catalog.EcosystemPaper:
		default:
			return fmt.Errorf("unsupported package ecosystem %q", ecosystem)
		}
		for ref, requirement := range packages {
			if err := normalizeRequirement(ref, &requirement); err != nil {
				return err
			}
			packages[ref] = requirement
		}
		if len(packages) == 0 {
			delete(m.Server.Packages, ecosystem)
		}
	}
	if m.MCDR != nil {
		if m.MCDR.Directory == "" {
			m.MCDR.Directory = "."
		}
		if err := SafeRelativePath(m.MCDR.Directory); err != nil {
			return err
		}
		m.MCDR.Directory = filepath.ToSlash(filepath.Clean(m.MCDR.Directory))
		if m.MCDR.Version == "" {
			m.MCDR.Version = version.Any
		}
		if m.MCDR.Packages == nil {
			m.MCDR.Packages = map[string]Requirement{}
		}
		for ref, requirement := range m.MCDR.Packages {
			if err := normalizeRequirement(ref, &requirement); err != nil {
				return err
			}
			m.MCDR.Packages[ref] = requirement
		}
	}
	if err := NormalizeTrackedFiles(m.Files); err != nil {
		return err
	}
	return nil
}

func normalizeRequirement(ref string, requirement *Requirement) error {
	if _, _, err := ParseReference(ref); err != nil {
		return err
	}
	if requirement.Selector == "" {
		requirement.Selector = version.Any
	}
	if requirement.Enabled == nil {
		enabled := true
		requirement.Enabled = &enabled
	}
	return nil
}

// DecodeManifest validates manifest bytes from a reader.
func DecodeManifest(reader io.Reader) (*Manifest, error) {
	m := &Manifest{}
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("manifest must contain one YAML document")
		}
		return nil, fmt.Errorf("decode manifest tail: %w", err)
	}
	if err := m.Normalize(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manifest) Fingerprint() (string, error) {
	if err := m.Normalize(); err != nil {
		return "", err
	}
	data, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CanonicalBytes returns the canonical YAML bytes of the document, including
// the schema header. It writes nothing.
func (m *Manifest) CanonicalBytes() ([]byte, error) {
	if err := m.Normalize(); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, err
	}
	return append([]byte("# yaml-language-server: $schema="+ManifestSchemaURL+"\n"), data...), nil
}
