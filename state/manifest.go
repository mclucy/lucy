package state

import (
	"bytes"
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
			Version: SupportedVersion,
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
	if err := ValidateVersion(m.Format.Version); err != nil {
		if IsVersionError(err) {
			return versionStateError(ManifestFile, "format.version", m.Format.Version, ErrVersionUnsupported)
		}
		return versionStateError(ManifestFile, "format.version", m.Format.Version, ErrMalformed)
	}

	if err := validateManifestPlatform(m.Environment.Platform); err != nil {
		return err
	}
	if m.Layout.ModsDir == "" {
		return NewStateError(ManifestFile, ErrMalformed, "layout.mods_dir", "layout.mods_dir is required")
	}
	if m.Layout.PluginsDir == "" {
		return NewStateError(ManifestFile, ErrMalformed, "layout.plugins_dir", "layout.plugins_dir is required")
	}
	if m.Layout.ConfigDir == "" {
		return NewStateError(ManifestFile, ErrMalformed, "layout.config_dir", "layout.config_dir is required")
	}
	if len(m.Policy.ManagedRoots) == 0 {
		return NewStateError(ManifestFile, ErrMalformed, "policy.managed_roots", "policy.managed_roots is required")
	}

	for i, custom := range m.Sources.Custom {
		if strings.TrimSpace(custom.Name) == "" {
			return NewStateError(ManifestFile, ErrMalformed, fmt.Sprintf("sources.custom[%d].name", i), "name is required")
		}
		if strings.TrimSpace(custom.URL) == "" {
			return NewStateError(ManifestFile, ErrMalformed, fmt.Sprintf("sources.custom[%d].url", i), "url is required")
		}
		if strings.TrimSpace(custom.Type) == "" {
			return NewStateError(ManifestFile, ErrMalformed, fmt.Sprintf("sources.custom[%d].type", i), "type is required")
		}
	}

	for i, pkg := range m.Packages {
		if err := validateManifestPackage(pkg); err != nil {
			return malformedStateError(ManifestFile, fmt.Sprintf("packages[%d]", i), err)
		}
	}

	for i, bundle := range m.Bundles {
		if err := validateManifestBundle(bundle); err != nil {
			return malformedStateError(ManifestFile, fmt.Sprintf("bundles[%d]", i), err)
		}
	}

	return nil
}

func validateManifestPlatform(value string) error {
	platform := types.Platform(strings.TrimSpace(value))
	if platform == "" {
		return fmt.Errorf("environment.platform is required")
	}

	switch platform {
	case types.PlatformFabric, types.PlatformNeoforge, types.PlatformForge, types.PlatformMCDR, types.PlatformNone:
		return nil
	default:
		return fmt.Errorf("invalid environment.platform %q", value)
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
	var buf bytes.Buffer
	writeTomlSectionHeader(&buf, "format")
	writeTomlStringField(&buf, "version", m.Format.Version)

	buf.WriteString("\n")
	writeTomlSectionHeader(&buf, "environment")
	writeTomlStringField(&buf, "game_version", m.Environment.GameVersion)
	writeTomlStringField(&buf, "platform", m.Environment.Platform)
	writeTomlStringField(&buf, "platform_version", m.Environment.PlatformVersion)

	buf.WriteString("\n")
	writeTomlSectionHeader(&buf, "sources")
	for _, custom := range m.Sources.Custom {
		writeTomlArrayTableHeader(&buf, "sources.custom")
		writeTomlStringField(&buf, "name", custom.Name)
		writeTomlStringField(&buf, "url", custom.URL)
		writeTomlStringField(&buf, "type", custom.Type)
		buf.WriteString("\n")
	}
	trimTrailingBlankLine(&buf)

	buf.WriteString("\n")
	writeTomlSectionHeader(&buf, "layout")
	writeTomlStringField(&buf, "mods_dir", m.Layout.ModsDir)
	writeTomlStringField(&buf, "plugins_dir", m.Layout.PluginsDir)
	writeTomlStringField(&buf, "config_dir", m.Layout.ConfigDir)

	buf.WriteString("\n")
	writeTomlSectionHeader(&buf, "policy")
	writeTomlStringSliceField(&buf, "managed_roots", m.Policy.ManagedRoots)
	writeTomlStringSliceField(&buf, "unmanaged_paths", m.Policy.UnmanagedPaths)

	for _, pkg := range m.Packages {
		buf.WriteString("\n")
		writeTomlArrayTableHeader(&buf, "packages")
		writeTomlStringField(&buf, "id", pkg.ID)
		writeTomlStringField(&buf, "version", pkg.Version)
		writeTomlStringField(&buf, "source", pkg.Source)
		writeTomlStringField(&buf, "side", string(pkg.Side))
		writeTomlBoolField(&buf, "optional", pkg.Optional)
		writeTomlBoolField(&buf, "pinned", pkg.Pinned)
	}

	for _, bundle := range m.Bundles {
		buf.WriteString("\n")
		writeTomlArrayTableHeader(&buf, "bundles")
		writeTomlStringField(&buf, "name", bundle.Name)
		writeTomlStringField(&buf, "type", string(bundle.Type))
		writeTomlStringField(&buf, "path", bundle.Path)
		writeTomlStringField(&buf, "source", bundle.Source)
		writeTomlBoolField(&buf, "optional", bundle.Optional)
	}

	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func (m *Manifest) Unmarshal(data []byte) error {
	return toml.Unmarshal(data, m)
}

func trimTrailingBlankLine(buf *bytes.Buffer) {
	b := buf.Bytes()
	if len(b) < 2 {
		return
	}
	if bytes.HasSuffix(b, []byte("\n\n")) {
		buf.Truncate(len(b) - 1)
	}
}
