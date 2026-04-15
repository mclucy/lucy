package state

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
	"github.com/pelletier/go-toml"
)

// Manifest stores the desired environment intent for a Lucy project.
// It is persisted in .lucy/manifest.toml.
//
// Manifest OWNS: intent.direct-roots, intent.managed-scope, intent.environment
// Manifest MUST NOT own: resolution.graph, artifact.hashes,
// artifact.download-urls
type Manifest struct {
	Format      ManifestFormat      `toml:"format"`
	Environment ManifestEnvironment `toml:"environment"`
	Sources     ManifestSources     `toml:"sources"`
	Layout      ManifestLayout      `toml:"layout"`
	Policy      ManifestPolicy      `toml:"policy"`
	Packages    []ManifestPackage   `toml:"packages"`
	Bundles     []ManifestBundle    `toml:"bundles"`
}

type ManifestFormat struct {
	Version string `toml:"version"`
}

type ManifestEnvironment struct {
	GameVersion     string `toml:"game_version"`
	Platform        string `toml:"platform"`
	PlatformVersion string `toml:"platform_version"`
}

type ManifestSources struct {
	Custom []CustomSource `toml:"custom"`
}

type CustomSource struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
	Type string `toml:"type"`
}

type ManifestLayout struct {
	ModsDir    string `toml:"mods_dir"`
	PluginsDir string `toml:"plugins_dir"`
	ConfigDir  string `toml:"config_dir"`
}

type ManifestPolicy struct {
	ManagedRoots   []string `toml:"managed_roots"`
	UnmanagedPaths []string `toml:"unmanaged_paths"`
}

type ManifestSide string

const (
	SideServer  ManifestSide = "server"
	SideClient  ManifestSide = "client"
	SideBoth    ManifestSide = "both"
	SideUnknown ManifestSide = "unknown"
)

type ManifestPackage struct {
	ID       string       `toml:"id"`
	Version  string       `toml:"version"`
	Source   string       `toml:"source"`
	Side     ManifestSide `toml:"side"`
	Optional bool         `toml:"optional"`
	Pinned   bool         `toml:"pinned"`
}

type BundleType string

const (
	BundleTypeConfig       BundleType = "config"
	BundleTypeDatapack     BundleType = "datapack"
	BundleTypeResourcepack BundleType = "resourcepack"
	BundleTypeKubeJS       BundleType = "kubejs"
	BundleTypeCustom       BundleType = "custom"
)

type ManifestBundle struct {
	Name     string     `toml:"name"`
	Type     BundleType `toml:"type"`
	Path     string     `toml:"path"`
	Source   string     `toml:"source"`
	Optional bool       `toml:"optional"`
}

func ManifestDefaults() Manifest {
	return Manifest{
		Format: ManifestFormat{
			Version: "v1",
		},
		Environment: ManifestEnvironment{
			GameVersion:     "",
			Platform:        string(types.PlatformNone),
			PlatformVersion: "",
		},
		Sources: ManifestSources{
			Custom: []CustomSource{},
		},
		Layout: ManifestLayout{
			ModsDir:    "mods",
			PluginsDir: "plugins",
			ConfigDir:  "config",
		},
		Policy: ManifestPolicy{
			ManagedRoots:   []string{"mods", "plugins"},
			UnmanagedPaths: []string{},
		},
		Packages: []ManifestPackage{},
		Bundles:  []ManifestBundle{},
	}
}

func ValidateManifest(m Manifest) error {
	if m.Format.Version == "" {
		return fmt.Errorf("manifest: format.version is required")
	}
	if m.Format.Version != "v1" {
		return fmt.Errorf("manifest: unsupported format.version %q", m.Format.Version)
	}

	if err := validateManifestPlatform(m.Environment.Platform); err != nil {
		return err
	}
	if m.Layout.ModsDir == "" {
		return fmt.Errorf("manifest: layout.mods_dir is required")
	}
	if m.Layout.PluginsDir == "" {
		return fmt.Errorf("manifest: layout.plugins_dir is required")
	}
	if m.Layout.ConfigDir == "" {
		return fmt.Errorf("manifest: layout.config_dir is required")
	}
	if len(m.Policy.ManagedRoots) == 0 {
		return fmt.Errorf("manifest: policy.managed_roots is required")
	}

	for i, custom := range m.Sources.Custom {
		if strings.TrimSpace(custom.Name) == "" {
			return fmt.Errorf("manifest: sources.custom[%d].name is required", i)
		}
		if strings.TrimSpace(custom.URL) == "" {
			return fmt.Errorf("manifest: sources.custom[%d].url is required", i)
		}
		if strings.TrimSpace(custom.Type) == "" {
			return fmt.Errorf("manifest: sources.custom[%d].type is required", i)
		}
	}

	for i, pkg := range m.Packages {
		if err := validateManifestPackage(pkg); err != nil {
			return fmt.Errorf("manifest: packages[%d]: %w", i, err)
		}
	}

	for i, bundle := range m.Bundles {
		if err := validateManifestBundle(bundle); err != nil {
			return fmt.Errorf("manifest: bundles[%d]: %w", i, err)
		}
	}

	return nil
}

func validateManifestPlatform(value string) error {
	platform := types.Platform(strings.TrimSpace(value))
	if platform == "" {
		return fmt.Errorf("manifest: environment.platform is required")
	}

	switch platform {
	case types.PlatformFabric, types.PlatformNeoforge, types.PlatformForge, types.PlatformMCDR, types.PlatformNone:
		return nil
	default:
		return fmt.Errorf("manifest: invalid environment.platform %q", value)
	}
}

func validateManifestPackage(pkg ManifestPackage) error {
	if strings.TrimSpace(pkg.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if strings.Contains(pkg.ID, "@") {
		return fmt.Errorf("id must use platform/name format without version")
	}
	parts := strings.Split(pkg.ID, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("id must use platform/name format")
	}
	platform := types.Platform(parts[0])
	if !platform.Valid() || platform == types.PlatformAny || platform == types.PlatformMinecraft || platform == types.PlatformUnknown {
		return fmt.Errorf("invalid package platform %q", parts[0])
	}

	if strings.TrimSpace(pkg.Version) == "" {
		return fmt.Errorf("version is required")
	}
	version := types.RawVersion(pkg.Version)
	if version.IsInvalid() {
		return fmt.Errorf("invalid version %q", pkg.Version)
	}

	if types.ParseSource(pkg.Source) == types.SourceUnknown {
		return fmt.Errorf("invalid source %q", pkg.Source)
	}

	switch pkg.Side {
	case SideServer, SideClient, SideBoth, SideUnknown:
	default:
		return fmt.Errorf("invalid side %q", pkg.Side)
	}

	return nil
}

func validateManifestBundle(bundle ManifestBundle) error {
	if strings.TrimSpace(bundle.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(bundle.Path) == "" {
		return fmt.Errorf("path is required")
	}
	if strings.TrimSpace(bundle.Source) == "" {
		return fmt.Errorf("source is required")
	}

	switch bundle.Type {
	case BundleTypeConfig, BundleTypeDatapack, BundleTypeResourcepack, BundleTypeKubeJS, BundleTypeCustom:
		return nil
	default:
		return fmt.Errorf("invalid type %q", bundle.Type)
	}
}

func (m Manifest) Marshal() ([]byte, error) {
	return toml.Marshal(m)
}

func (m *Manifest) Unmarshal(data []byte) error {
	return toml.Unmarshal(data, m)
}
