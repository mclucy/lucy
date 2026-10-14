package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

const (
	Filename      = "lucy.yaml"
	SchemaVersion = 1
)

type Document struct {
	SchemaVersion int                 `yaml:"manifest_version" json:"manifest_version"`
	Server        Server              `yaml:"server" json:"server"`
	MCDR          *MCDR               `yaml:"mcdr,omitempty" json:"mcdr,omitempty"`
	Files         []types.TrackedFile `yaml:"files,omitempty" json:"files,omitempty"`
}

type Core struct {
	Product        types.ServerProduct `yaml:"product" json:"product"`
	ProductVersion types.Version       `yaml:"product_version,omitempty" json:"product_version"`
	Repository     string              `yaml:"repository,omitempty" json:"repository,omitempty"`
	FilePattern    string              `yaml:"file_pattern,omitempty" json:"file_pattern,omitempty"`
}

type Server struct {
	Minecraft types.Version                              `yaml:"minecraft" json:"minecraft"`
	Directory string                                     `yaml:"directory,omitempty" json:"directory"`
	Core      Core                                       `yaml:"core" json:"core"`
	Packages  map[types.Ecosystem]map[string]Requirement `yaml:"packages" json:"packages"`
}

type MCDR struct {
	Directory string                 `yaml:"directory,omitempty" json:"directory"`
	Version   types.Version          `yaml:"version,omitempty" json:"version"`
	Packages  map[string]Requirement `yaml:"packages" json:"packages"`
}

type Requirement struct {
	Selector    types.Version `yaml:"selector,omitempty" json:"selector"`
	FilePattern string        `yaml:"file_pattern,omitempty" json:"file_pattern,omitempty"`
	Enabled     *bool         `yaml:"enabled,omitempty" json:"enabled"`
}

func (r Requirement) IsEnabled() bool { return r.Enabled == nil || *r.Enabled }

func (r *Requirement) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		if n.Tag != "!!str" {
			return fmt.Errorf("package selector must be a quoted string")
		}
		r.Selector = types.Version(n.Value)
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
func ParseReference(ref string) (provider types.Provider, project string, err error) {
	providerKey, project, ok := strings.Cut(ref, ":")
	if !ok || project == "" || strings.Contains(ref, "@") {
		return types.ProviderUnknown, "", fmt.Errorf("require provider:project reference, got %q", ref)
	}
	provider, ok = types.ParseProvider(providerKey)
	if !ok || !isPackageProvider(provider) {
		return types.ProviderUnknown, "", fmt.Errorf("unsupported package provider %q", providerKey)
	}
	return provider, project, nil
}

func isPackageProvider(provider types.Provider) bool {
	switch provider {
	case types.ProviderModrinth, types.ProviderCurseForge, types.ProviderMCDR:
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

func Normalize(d *Document) error {
	if d.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported manifest version %d", d.SchemaVersion)
	}
	d.Server.Minecraft = types.Version(strings.TrimSpace(d.Server.Minecraft.String()))
	if declared := d.Server.Minecraft; declared == "" || declared.CanInfer() || declared.IsInvalid() {
		return fmt.Errorf("server.minecraft must specify a concrete Minecraft version")
	}
	d.Server.Core.Product = types.ServerProduct(strings.ToLower(strings.TrimSpace(d.Server.Core.Product.String())))
	if d.Server.Core.Product == types.ServerProductUnknown {
		return fmt.Errorf("server core product is required")
	}
	if product, known := types.ParseServerProduct(d.Server.Core.Product.String()); known {
		d.Server.Core.Product = product
	} else if d.Server.Core.Repository == "" {
		return fmt.Errorf("unsupported server product %q; GitHub forks require core.repository", d.Server.Core.Product)
	}
	if d.Server.Core.ProductVersion == "" {
		d.Server.Core.ProductVersion = types.VersionAny
	}
	if d.Server.Directory == "" {
		d.Server.Directory = "."
	}
	if err := SafeRelativePath(d.Server.Directory); err != nil {
		return err
	}
	d.Server.Directory = filepath.ToSlash(filepath.Clean(d.Server.Directory))
	if d.Server.Packages == nil {
		d.Server.Packages = map[types.Ecosystem]map[string]Requirement{}
	}
	for ecosystem, packages := range d.Server.Packages {
		switch ecosystem {
		case types.EcosystemFabric,
			types.EcosystemForge,
			types.EcosystemNeoForge,
			types.EcosystemBukkit,
			types.EcosystemPaper:
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
			delete(d.Server.Packages, ecosystem)
		}
	}
	if d.MCDR != nil {
		if d.MCDR.Directory == "" {
			d.MCDR.Directory = "."
		}
		if err := SafeRelativePath(d.MCDR.Directory); err != nil {
			return err
		}
		d.MCDR.Directory = filepath.ToSlash(filepath.Clean(d.MCDR.Directory))
		if d.MCDR.Version == "" {
			d.MCDR.Version = types.VersionAny
		}
		if d.MCDR.Packages == nil {
			d.MCDR.Packages = map[string]Requirement{}
		}
		for ref, requirement := range d.MCDR.Packages {
			if err := normalizeRequirement(ref, &requirement); err != nil {
				return err
			}
			d.MCDR.Packages[ref] = requirement
		}
	}
	if err := input.NormalizeTrackedFiles(d.Files); err != nil {
		return err
	}
	return nil
}

func normalizeRequirement(ref string, requirement *Requirement) error {
	if _, _, err := ParseReference(ref); err != nil {
		return err
	}
	if requirement.Selector == "" {
		requirement.Selector = types.VersionAny
	}
	if requirement.Enabled == nil {
		enabled := true
		requirement.Enabled = &enabled
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

// Decode validates manifest bytes from a reader.
func Decode(reader io.Reader) (*Document, error) {
	d := &Document{}
	decoder := yaml.NewDecoder(reader)
	decoder.KnownFields(true)
	if err := decoder.Decode(d); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("manifest must contain one YAML document")
		}
		return nil, fmt.Errorf("decode manifest tail: %w", err)
	}
	if err := Normalize(d); err != nil {
		return nil, err
	}
	return d, nil
}

func Fingerprint(d *Document) (string, error) {
	if err := Normalize(d); err != nil {
		return "", err
	}
	data, err := json.Marshal(d, json.Deterministic(true))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CanonicalBytes returns the canonical YAML bytes of the document, including
// the schema header. It writes nothing.
func CanonicalBytes(d *Document) ([]byte, error) {
	if err := Normalize(d); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(d)
	if err != nil {
		return nil, err
	}
	return append([]byte("# yaml-language-server: $schema="+SchemaURL+"\n"), data...), nil
}

// Write publishes the canonical bytes at the manifest location of root.
func Write(root string, d *Document) error {
	data, err := CanonicalBytes(d)
	if err != nil {
		return err
	}
	path := filepath.Join(root, Filename)
	if err := fsutil.AtomicWrite(path, data, 0o644); err != nil {
		return err
	}
	log.Info(
		"manifest written",
		"path", path,
		"version", d.SchemaVersion,
	)
	return nil
}
