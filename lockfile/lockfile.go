package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

const (
	Filename      = "lucy-lock.yaml"
	FormatVersion = 1
)

type Document struct {
	Version             int                   `yaml:"lock_version"`
	ManifestFingerprint string                `yaml:"manifest_fingerprint"`
	Server              Server                `yaml:"server"`
	MCDR                *MCDR                 `yaml:"mcdr,omitempty"`
	Packages            []types.LockedPackage `yaml:"packages"`
}

type Server struct {
	Minecraft      string `yaml:"minecraft"`
	Directory      string `yaml:"directory"`
	Distribution   string `yaml:"distribution"`
	CoreVersion    string `yaml:"core_version"`
	types.Artifact `yaml:",inline"`
	Bootstrap      string                 `yaml:"bootstrap,omitempty"`
	Inputs         []types.BootstrapInput `yaml:"inputs,omitempty"`
	Java           int                    `yaml:"java"`
}

type MCDR struct {
	Directory string                 `yaml:"directory"`
	Version   string                 `yaml:"version"`
	Python    string                 `yaml:"python"`
	Platform  string                 `yaml:"platform"`
	Inputs    []types.BootstrapInput `yaml:"inputs"`
}

func Validate(d *Document) error {
	if d.Version != FormatVersion {
		return fmt.Errorf("unsupported lock version %d", d.Version)
	}
	if !strings.HasPrefix(d.ManifestFingerprint, "sha256:") || len(d.ManifestFingerprint) != 71 {
		return fmt.Errorf("invalid manifest fingerprint")
	}
	if d.Server.Minecraft == "" || d.Server.Distribution == "" || d.Server.CoreVersion == "" {
		return fmt.Errorf("lock requires exact server versions")
	}
	if err := manifest.SafePath(d.Server.Directory); err != nil {
		return err
	}
	if err := validateArtifact(d.Server.Artifact); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	for _, input := range d.Server.Inputs {
		if err := validateInput(input); err != nil {
			return err
		}
	}
	if d.MCDR != nil {
		if err := manifest.SafePath(d.MCDR.Directory); err != nil {
			return err
		}
		if d.MCDR.Version == "" || d.MCDR.Python == "" || len(d.MCDR.Inputs) == 0 {
			return fmt.Errorf("incomplete MCDR environment resolution")
		}
		for _, input := range d.MCDR.Inputs {
			if err := validateInput(input); err != nil {
				return err
			}
		}
	}
	for _, pkg := range d.Packages {
		if pkg.Runtime != "server" && pkg.Runtime != "mcdr" {
			return fmt.Errorf("unknown package runtime %q", pkg.Runtime)
		}
		active := len(pkg.Requirements) == 0
		for _, req := range pkg.Requirements {
			if _, _, err := manifest.ParseReference(req.Reference); err != nil {
				return err
			}
			active = active || req.Enabled
		}
		if active {
			if err := validateArtifact(pkg.Artifact); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateInput(input types.BootstrapInput) error {
	if err := manifest.SafePath(input.Path); err != nil {
		return err
	}
	return validateArtifact(input.Artifact)
}

func validateArtifact(a types.Artifact) error {
	if a.Provider == "" || (a.URL == "" && a.Embedded == "") || a.Filename == "" || a.Version == "" {
		return fmt.Errorf("incomplete artifact coordinates")
	}
	if filepath.Base(a.Filename) != a.Filename || strings.ContainsAny(a.Filename, "/\\") {
		return fmt.Errorf("unsafe artifact filename %q", a.Filename)
	}
	if len(a.Hashes["sha256"]) != 64 {
		return fmt.Errorf("artifact %s requires a SHA-256 digest", a.Filename)
	}
	return nil
}

func Read(root string) (*Document, error) {
	data, err := os.ReadFile(filepath.Join(root, Filename))
	if err != nil {
		return nil, err
	}
	d := &Document{}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(d); err != nil {
		return nil, fmt.Errorf("decode lock: %w", err)
	}
	if err := Validate(d); err != nil {
		return nil, err
	}
	return d, nil
}

func Write(root string, d *Document) error {
	if err := Validate(d); err != nil {
		return err
	}
	data, err := yaml.Marshal(d)
	if err != nil {
		return err
	}
	return manifest.AtomicWrite(filepath.Join(root, Filename), data)
}

// ContextReference resolves only unique native module names from this lock.
func (d *Document) ContextReference(runtime string, loader types.Ecosystem, name string) (string, error) {
	matches := map[string]struct{}{}
	for _, p := range d.Packages {
		if p.Runtime != runtime || (loader != types.EcoUnspecified && p.Loader != loader) {
			continue
		}
		for _, m := range p.Modules {
			matched := m.ID == name
			for _, alias := range m.Provides {
				matched = matched || alias == name
			}
			if matched {
				for _, req := range p.Requirements {
					if req.Enabled {
						matches[req.Reference] = struct{}{}
					}
				}
			}
		}
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous package shorthand %q; specify provider:project", name)
	}
	for ref := range matches {
		return ref, nil
	}
	return "", nil
}
