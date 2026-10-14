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
	FormatVersion = 1
)

type Document struct {
	Version int                 `yaml:"manifest_version" json:"manifest_version"`
	Server  Server              `yaml:"server" json:"server"`
	MCDR    *MCDR               `yaml:"mcdr,omitempty" json:"mcdr,omitempty"`
	Files   []types.TrackedFile `yaml:"files,omitempty" json:"files,omitempty"`
}

type Core struct {
	Distribution string `yaml:"distribution" json:"distribution"`
	Version      string `yaml:"version,omitempty" json:"version"`
	Repository   string `yaml:"repository,omitempty" json:"repository,omitempty"`
	File         string `yaml:"file,omitempty" json:"file,omitempty"`
}

type Server struct {
	Minecraft string                                     `yaml:"minecraft" json:"minecraft"`
	Directory string                                     `yaml:"directory,omitempty" json:"directory"`
	Core      Core                                       `yaml:"core" json:"core"`
	Packages  map[types.Ecosystem]map[string]Requirement `yaml:"packages,omitempty" json:"packages"`
}

type MCDR struct {
	Directory string                 `yaml:"directory,omitempty" json:"directory"`
	Version   string                 `yaml:"version,omitempty" json:"version"`
	Packages  map[string]Requirement `yaml:"packages,omitempty" json:"packages"`
}

type Requirement struct {
	Version string `yaml:"version,omitempty" json:"version"`
	File    string `yaml:"file,omitempty" json:"file,omitempty"`
	Enabled *bool  `yaml:"enabled,omitempty" json:"enabled"`
}

func (r Requirement) IsEnabled() bool { return r.Enabled == nil || *r.Enabled }
func (r Requirement) Selector() string {
	if r.Version == "" {
		return "stable"
	}
	return r.Version
}

func (r *Requirement) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		if n.Tag != "!!str" {
			return fmt.Errorf("package selector must be a quoted string")
		}
		r.Version = n.Value
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("package requirement must be a string or object")
	}
	for i := 0; i < len(n.Content); i += 2 {
		switch n.Content[i].Value {
		case "version", "file", "enabled":
		default:
			return fmt.Errorf("unknown package requirement field %q", n.Content[i].Value)
		}
	}
	type plain Requirement
	return n.Decode((*plain)(r))
}

func (r Requirement) MarshalYAML() (any, error) {
	if r.File == "" && r.IsEnabled() {
		return r.Selector(), nil
	}
	type plain Requirement
	return plain(r), nil
}

func ParseReference(ref string) (provider, project string, err error) {
	provider, project, ok := strings.Cut(ref, ":")
	if !ok || project == "" || strings.Contains(ref, "@") {
		return "", "", fmt.Errorf("require provider:project reference, got %q", ref)
	}
	switch provider {
	case "modrinth", "curseforge", "mcdr":
	default:
		return "", "", fmt.Errorf("unsupported package provider %q", provider)
	}
	return provider, project, nil
}

func SafePath(path string) error {
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
	if d.Version != FormatVersion {
		return fmt.Errorf("unsupported manifest version %d", d.Version)
	}
	d.Server.Minecraft = strings.TrimSpace(d.Server.Minecraft)
	declared := types.BareVersion(d.Server.Minecraft)
	if declared == "" || declared.CanInfer() || declared.IsInvalid() {
		return fmt.Errorf("server.minecraft must specify a concrete Minecraft version")
	}
	d.Server.Core.Distribution = strings.ToLower(strings.TrimSpace(d.Server.Core.Distribution))
	switch d.Server.Core.Distribution {
	case "vanilla", "fabric", "forge", "neoforge", "paper", "purpur":
	default:
		if d.Server.Core.Repository == "" {
			return fmt.Errorf("unsupported server distribution %q; GitHub forks require core.repository", d.Server.Core.Distribution)
		}
	}
	if d.Server.Core.Version == "" {
		d.Server.Core.Version = "stable"
	}
	if d.Server.Directory == "" {
		d.Server.Directory = "."
	}
	if err := SafePath(d.Server.Directory); err != nil {
		return err
	}
	d.Server.Directory = filepath.ToSlash(filepath.Clean(d.Server.Directory))
	if d.Server.Packages == nil {
		d.Server.Packages = map[types.Ecosystem]map[string]Requirement{}
	}
	for loader, packages := range d.Server.Packages {
		switch loader {
		case types.EcoFabric, types.EcoForge, types.EcoNeoforge, types.EcoBukkit:
		default:
			return fmt.Errorf("unsupported server package loader %q", loader)
		}
		for ref, r := range packages {
			if err := normalizeRequirement(ref, &r); err != nil {
				return err
			}
			packages[ref] = r
		}
		if len(packages) == 0 {
			delete(d.Server.Packages, loader)
		}
	}
	if d.MCDR != nil {
		if d.MCDR.Directory == "" {
			d.MCDR.Directory = "."
		}
		if err := SafePath(d.MCDR.Directory); err != nil {
			return err
		}
		d.MCDR.Directory = filepath.ToSlash(filepath.Clean(d.MCDR.Directory))
		if d.MCDR.Version == "" {
			d.MCDR.Version = "stable"
		}
		if d.MCDR.Packages == nil {
			d.MCDR.Packages = map[string]Requirement{}
		}
		for ref, r := range d.MCDR.Packages {
			if err := normalizeRequirement(ref, &r); err != nil {
				return err
			}
			d.MCDR.Packages[ref] = r
		}
	}
	if err := input.NormalizeTrackedFiles(d.Files); err != nil {
		return err
	}
	return nil
}

func normalizeRequirement(ref string, r *Requirement) error {
	if _, _, err := ParseReference(ref); err != nil {
		return err
	}
	r.Version = r.Selector()
	if r.Enabled == nil {
		r.Enabled = new(true)
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

// Snapshot returns the canonical bytes of the document, including the schema
// modeline. It writes nothing. A publisher that decides when and where the
// bytes land takes a snapshot first; Write takes one and publishes it.
func Snapshot(d *Document) ([]byte, error) {
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
	data, err := Snapshot(d)
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
		"version", d.Version,
	)
	return nil
}
