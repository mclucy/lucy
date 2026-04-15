package state

import (
	"bytes"
	"fmt"
	"sort"
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
	// CompatiblePlatforms lists extra compatibility layers or controller surfaces
	// that can coexist with the primary runtime without replacing it.
	//
	// Example: a NeoForge runtime with Sinytra and MCDR support can be modeled as
	// platform="neoforge" plus compatible_platforms=["fabric", "mcdr", "sinytra"].
	CompatiblePlatforms []string `toml:"compatible_platforms"`
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
	ID string `toml:"id"`
	// Version stores version intent exactly as written in the manifest.
	//
	// It may be an exact version or a fuzzy selector such as "latest",
	// "compatible", or a future range/non-exact preference. The manifest is the
	// intent layer, so Lucy must preserve this string verbatim instead of
	// rewriting it to the currently resolved exact version.
	Version string `toml:"version"`
	Source  string `toml:"source"`
	// Role defines how Lucy should treat this package in desired state.
	//
	// - required: explicit operator intent, including user-selected leaf nodes during adopt
	// - transitive: resolver-derived dependency Lucy may auto-prune when no longer needed
	// - ignored: known content Lucy sees but must leave outside sync responsibility
	//
	// Non-leaf nodes remain visible to init/adopt users because Minecraft package
	// boundaries are often fuzzy, but that visibility must not become a fourth role.
	Role     ManifestRole `toml:"role"`
	Side     ManifestSide `toml:"side"`
	Optional bool         `toml:"optional"`
	Pinned   bool         `toml:"pinned"`
}

type ManifestRole string

const (
	RoleRequired   ManifestRole = "required"
	RoleTransitive ManifestRole = "transitive"
	RoleIgnored    ManifestRole = "ignored"
)

type ClassifiedPackage struct {
	ID       string
	Version  string
	Source   string
	Role     ManifestRole
	Side     ManifestSide
	Optional bool
	Pinned   bool
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
			GameVersion:         "",
			Platform:            string(types.PlatformNone),
			PlatformVersion:     "",
			CompatiblePlatforms: []string{},
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

	if err := ValidateManifestEnvironment(m.Environment); err != nil {
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

func ValidateManifestEnvironment(env ManifestEnvironment) error {
	platform := strings.TrimSpace(env.Platform)
	if err := validateManifestPlatform(platform); err != nil {
		return err
	}
	if err := validateCompatiblePlatforms(platform, env.CompatiblePlatforms); err != nil {
		return err
	}
	return nil
}

func CompatiblePlatformOptions(primary string) []string {
	switch types.Platform(strings.TrimSpace(primary)) {
	case types.PlatformNeoforge:
		return []string{"fabric", "mcdr", "sinytra"}
	case types.PlatformFabric, types.PlatformForge, types.PlatformNone:
		return []string{"mcdr"}
	case types.PlatformMCDR:
		return nil
	default:
		return nil
	}
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

func validateCompatiblePlatforms(primary string, platforms []string) error {
	allowed := CompatiblePlatformOptions(primary)
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, platform := range allowed {
		allowedSet[platform] = struct{}{}
	}

	seen := make(map[string]struct{}, len(platforms))
	for i, raw := range platforms {
		platform := strings.TrimSpace(raw)
		if platform == "" {
			return fmt.Errorf("environment.compatible_platforms[%d] is required", i)
		}
		if platform == strings.TrimSpace(primary) {
			return fmt.Errorf("environment.compatible_platforms must not repeat primary platform %q", platform)
		}
		if _, ok := seen[platform]; ok {
			return fmt.Errorf("environment.compatible_platforms contains duplicate %q", platform)
		}
		seen[platform] = struct{}{}
		if _, ok := allowedSet[platform]; !ok {
			allowedText := "none"
			if len(allowed) > 0 {
				allowedText = strings.Join(allowed, ", ")
			}
			return fmt.Errorf("environment.compatible_platforms %q is incompatible with primary platform %q; allowed: %s", platform, primary, allowedText)
		}
	}

	if slicesContains(platforms, "sinytra") && !slicesContains(platforms, "fabric") {
		return fmt.Errorf("environment.compatible_platforms " + `"sinytra" requires "fabric" compatibility to also be selected`)
	}

	return nil
}

func slicesContains(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
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

	switch pkg.Role {
	case RoleRequired, RoleTransitive, RoleIgnored:
	case "":
		return fmt.Errorf("role is required")
	default:
		return fmt.Errorf("invalid role %q; expected one of required, transitive, ignored", pkg.Role)
	}

	switch pkg.Side {
	case SideServer, SideClient, SideBoth, SideUnknown:
	default:
		return fmt.Errorf("invalid side %q", pkg.Side)
	}

	return nil
}

func NormalizeManifestVersionIntent(version types.RawVersion) string {
	trimmed := strings.TrimSpace(version.String())
	switch trimmed {
	case "", "any", "none", "unknown":
		return types.VersionCompatible.String()
	default:
		return trimmed
	}
}

func UpsertManifestRequiredIntent(manifest *Manifest, id types.PackageId, source string) *Manifest {
	if manifest == nil {
		defaults := ManifestDefaults()
		manifest = &defaults
	} else {
		clone := *manifest
		clone.Environment.CompatiblePlatforms = append([]string(nil), manifest.Environment.CompatiblePlatforms...)
		clone.Sources.Custom = append([]CustomSource(nil), manifest.Sources.Custom...)
		clone.Policy.ManagedRoots = append([]string(nil), manifest.Policy.ManagedRoots...)
		clone.Policy.UnmanagedPaths = append([]string(nil), manifest.Policy.UnmanagedPaths...)
		clone.Packages = append([]ManifestPackage(nil), manifest.Packages...)
		clone.Bundles = append([]ManifestBundle(nil), manifest.Bundles...)
		manifest = &clone
	}

	resolvedSource := strings.TrimSpace(source)
	if types.ParseSource(resolvedSource) == types.SourceUnknown {
		resolvedSource = "auto"
	}
	intentVersion := NormalizeManifestVersionIntent(id.Version)

	for i := range manifest.Packages {
		if manifest.Packages[i].ID != id.StringPlatformName() {
			continue
		}
		manifest.Packages[i].Version = intentVersion
		manifest.Packages[i].Source = resolvedSource
		manifest.Packages[i].Role = RoleRequired
		if manifest.Packages[i].Side == "" {
			manifest.Packages[i].Side = SideUnknown
		}
		sort.Slice(manifest.Packages, func(i, j int) bool {
			return manifest.Packages[i].ID < manifest.Packages[j].ID
		})
		return manifest
	}

	manifest.Packages = append(manifest.Packages, ManifestPackage{
		ID:      id.StringPlatformName(),
		Version: intentVersion,
		Source:  resolvedSource,
		Role:    RoleRequired,
		Side:    SideUnknown,
	})
	sort.Slice(manifest.Packages, func(i, j int) bool {
		return manifest.Packages[i].ID < manifest.Packages[j].ID
	})
	return manifest
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

func ManifestPackagesFromClassified(classified []ClassifiedPackage) []ManifestPackage {
	packages := make([]ManifestPackage, 0, len(classified))
	for _, pkg := range classified {
		id := strings.TrimSpace(pkg.ID)
		if id == "" {
			continue
		}
		version := strings.TrimSpace(pkg.Version)
		if version == "" {
			version = types.VersionCompatible.String()
		}
		source := strings.TrimSpace(pkg.Source)
		if types.ParseSource(source) == types.SourceUnknown {
			source = "auto"
		}
		role := pkg.Role
		if role == "" {
			role = RoleTransitive
		}
		side := pkg.Side
		if side == "" {
			side = SideUnknown
		}
		packages = append(packages, ManifestPackage{
			ID:       id,
			Version:  version,
			Source:   source,
			Role:     role,
			Side:     side,
			Optional: pkg.Optional,
			Pinned:   pkg.Pinned,
		})
	}
	sort.Slice(packages, func(i, j int) bool {
		return packages[i].ID < packages[j].ID
	})
	return packages
}

func UpdateManifestRolesForAdd(manifest *Manifest, requested []types.PackageId, lock *Lock) *Manifest {
	base := cloneManifestOrDefaults(manifest)
	required := manifestPackagesByRole(base.Packages, RoleRequired)
	ignored := manifestPackagesByRole(base.Packages, RoleIgnored)

	for _, id := range requested {
		resolvedID := resolveManifestPackageID(id, &base, lock)
		if resolvedID == "" {
			continue
		}
		if _, keepIgnored := ignored[resolvedID]; keepIgnored {
			continue
		}

		pkg, ok := manifestPackageByID(base.Packages, resolvedID)
		if !ok {
			pkg = defaultManifestPackageForID(resolvedID)
		}
		pkg.ID = resolvedID
		pkg.Role = RoleRequired
		pkg.Version = requestedManifestVersion(id, pkg.Version)
		pkg.Source = normalizedManifestSource(pkg.Source)
		if pkg.Side == "" {
			pkg.Side = SideUnknown
		}
		required[resolvedID] = pkg
	}

	base.Packages = rebuildManifestPackages(required, ignored, lock)
	return &base
}

func UpdateManifestRolesForRemove(manifest *Manifest, removed []types.PackageId, lock *Lock) *Manifest {
	base := cloneManifestOrDefaults(manifest)
	required := manifestPackagesByRole(base.Packages, RoleRequired)
	ignored := manifestPackagesByRole(base.Packages, RoleIgnored)

	for _, id := range removed {
		resolvedID := resolveManifestPackageID(id, &base, lock)
		if resolvedID == "" {
			continue
		}
		if _, keepIgnored := ignored[resolvedID]; keepIgnored {
			continue
		}
		delete(required, resolvedID)
	}

	base.Packages = rebuildManifestPackages(required, ignored, lock)
	return &base
}

func cloneManifestOrDefaults(manifest *Manifest) Manifest {
	if manifest == nil {
		return ManifestDefaults()
	}

	cloned := *manifest
	cloned.Environment.CompatiblePlatforms = append([]string(nil), manifest.Environment.CompatiblePlatforms...)
	cloned.Sources.Custom = append([]CustomSource(nil), manifest.Sources.Custom...)
	cloned.Policy.ManagedRoots = append([]string(nil), manifest.Policy.ManagedRoots...)
	cloned.Policy.UnmanagedPaths = append([]string(nil), manifest.Policy.UnmanagedPaths...)
	cloned.Packages = append([]ManifestPackage(nil), manifest.Packages...)
	cloned.Bundles = append([]ManifestBundle(nil), manifest.Bundles...)
	normalizeManifest(&cloned)
	return cloned
}

func manifestPackagesByRole(packages []ManifestPackage, role ManifestRole) map[string]ManifestPackage {
	indexed := make(map[string]ManifestPackage)
	for _, pkg := range packages {
		if pkg.Role != role {
			continue
		}
		indexed[pkg.ID] = pkg
	}
	return indexed
}

func manifestPackageByID(packages []ManifestPackage, id string) (ManifestPackage, bool) {
	for _, pkg := range packages {
		if pkg.ID == id {
			return pkg, true
		}
	}
	return ManifestPackage{}, false
}

func rebuildManifestPackages(required map[string]ManifestPackage, ignored map[string]ManifestPackage, lock *Lock) []ManifestPackage {
	classified := make([]ClassifiedPackage, 0, len(required)+len(ignored))
	requiredIDs := make(map[string]struct{}, len(required))
	ignoredIDs := make(map[string]struct{}, len(ignored))

	for id, pkg := range required {
		requiredIDs[id] = struct{}{}
		classified = append(classified, ClassifiedPackage{
			ID:       id,
			Version:  pkg.Version,
			Source:   normalizedManifestSource(pkg.Source),
			Role:     RoleRequired,
			Side:     normalizedManifestSide(pkg.Side),
			Optional: pkg.Optional,
			Pinned:   pkg.Pinned,
		})
	}
	for id, pkg := range ignored {
		ignoredIDs[id] = struct{}{}
		classified = append(classified, ClassifiedPackage{
			ID:       id,
			Version:  pkg.Version,
			Source:   normalizedManifestSource(pkg.Source),
			Role:     RoleIgnored,
			Side:     normalizedManifestSide(pkg.Side),
			Optional: pkg.Optional,
			Pinned:   pkg.Pinned,
		})
	}

	if lock != nil {
		for _, locked := range lock.Packages {
			if _, keepIgnored := ignoredIDs[locked.ID]; keepIgnored {
				continue
			}
			if _, isRequired := requiredIDs[locked.ID]; isRequired {
				continue
			}
			if !lockedPackageReachableFromRequired(locked, requiredIDs) {
				continue
			}

			classified = append(classified, ClassifiedPackage{
				ID:       locked.ID,
				Version:  locked.Version,
				Source:   normalizedManifestSource(locked.Source),
				Role:     RoleTransitive,
				Side:     normalizedManifestSide(ManifestSide(locked.Side)),
				Optional: locked.Optional,
			})
		}
	}

	return ManifestPackagesFromClassified(classified)
}

func lockedPackageReachableFromRequired(pkg LockedPackage, required map[string]struct{}) bool {
	for _, step := range pkg.Provenance {
		id := normalizeProvenanceStep(step)
		if id == "" || id == "root" {
			continue
		}
		if _, ok := required[id]; ok {
			return true
		}
	}

	requester := normalizeProvenanceStep(pkg.Requester)
	if requester == "" || requester == "root" {
		return false
	}
	_, ok := required[requester]
	return ok
}

func normalizeProvenanceStep(step string) string {
	trimmed := strings.TrimSpace(step)
	if trimmed == "" || trimmed == "root" {
		return trimmed
	}
	if prefix, _, ok := strings.Cut(trimmed, "@"); ok {
		return prefix
	}
	return trimmed
}

func requestedManifestVersion(id types.PackageId, fallback string) string {
	if id.Version == types.VersionAny {
		if strings.TrimSpace(fallback) != "" {
			return fallback
		}
		return types.VersionCompatible.String()
	}
	return id.Version.String()
}

func normalizedManifestSource(source string) string {
	if types.ParseSource(source) == types.SourceUnknown {
		return "auto"
	}
	if source == "" {
		return "auto"
	}
	return source
}

func normalizedManifestSide(side ManifestSide) ManifestSide {
	switch side {
	case SideServer, SideClient, SideBoth, SideUnknown:
		return side
	default:
		return SideUnknown
	}
}

func defaultManifestPackageForID(id string) ManifestPackage {
	return ManifestPackage{
		ID:      id,
		Version: types.VersionCompatible.String(),
		Source:  "auto",
		Role:    RoleRequired,
		Side:    SideUnknown,
	}
}

func resolveManifestPackageID(id types.PackageId, manifest *Manifest, lock *Lock) string {
	if id.IsIdentityPackage() {
		id.NormalizeIdentityPackage()
	}
	if id.Platform != types.PlatformAny && id.Platform != types.PlatformUnknown {
		return id.StringPlatformName()
	}

	if manifest != nil {
		candidate := resolveIDByName(id.Name, manifestPackageIDs(manifest.Packages))
		if candidate != "" {
			return candidate
		}
	}
	if lock != nil {
		ids := make([]string, 0, len(lock.Packages))
		for _, pkg := range lock.Packages {
			ids = append(ids, pkg.ID)
		}
		candidate := resolveIDByName(id.Name, ids)
		if candidate != "" {
			return candidate
		}
	}

	return id.StringPlatformName()
}

func manifestPackageIDs(packages []ManifestPackage) []string {
	ids := make([]string, 0, len(packages))
	for _, pkg := range packages {
		ids = append(ids, pkg.ID)
	}
	return ids
}

func resolveIDByName(name types.ProjectName, ids []string) string {
	var match string
	for _, id := range ids {
		parts := strings.Split(id, "/")
		if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
			continue
		}
		if parts[1] != name.String() {
			continue
		}
		if match != "" && match != id {
			return ""
		}
		match = id
	}
	return match
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
	writeTomlStringSliceField(&buf, "compatible_platforms", m.Environment.CompatiblePlatforms)

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
		writeTomlStringField(&buf, "role", string(pkg.Role))
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
	if err := toml.Unmarshal(data, m); err != nil {
		return err
	}
	normalizeManifest(m)
	return nil
}

func normalizeManifest(m *Manifest) {
	if m == nil {
		return
	}
	if m.Environment.CompatiblePlatforms == nil {
		m.Environment.CompatiblePlatforms = []string{}
	}
	if m.Sources.Custom == nil {
		m.Sources.Custom = []CustomSource{}
	}
	if m.Policy.ManagedRoots == nil {
		m.Policy.ManagedRoots = []string{}
	}
	if m.Policy.UnmanagedPaths == nil {
		m.Policy.UnmanagedPaths = []string{}
	}
	if m.Packages == nil {
		m.Packages = []ManifestPackage{}
	}
	if m.Bundles == nil {
		m.Bundles = []ManifestBundle{}
	}
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
