package state

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mclucy/lucy/types"
)

// Manifest stores the desired environment intent for a Lucy project.
// It is persisted in lucy.yaml.
type Manifest struct {
	Environment ManifestEnvironment `yaml:"environment"`
	Packages    []ManifestPackage   `yaml:"packages"`
	Bundles     []ManifestBundle    `yaml:"bundles"`
	Config      *Config             `yaml:"config,omitempty"`
}

type ManifestEnvironment struct {
	GameVersion            string   `yaml:"game_version"`
	ServerCore             string   `yaml:"server_core"`
	ServerCoreVersion      string   `yaml:"server_core_version"`
	ModdingPlatform        string   `yaml:"modding_platform"`
	ModdingPlatformVersion string   `yaml:"modding_platform_version"`
	CompatiblePlatforms    []string `yaml:"compatible_platforms"`
	Mcdr                   bool     `yaml:"mcdr"`
	DeclaredCapabilities   []string `yaml:"declared_capabilities"`
}

type ManifestSide string

const (
	SideServer  ManifestSide = "server"
	SideClient  ManifestSide = "client"
	SideBoth    ManifestSide = "both"
	SideUnknown ManifestSide = "unknown"
)

type ManifestPackage struct {
	ID string `yaml:"id"`
	// Version stores version intent exactly as written in the manifest.
	//
	// It may be an exact version, a range, or one of the fuzzy selectors "any",
	// "stable", and "beta". The manifest is the
	// intent layer, so Lucy must preserve this string verbatim instead of
	// rewriting it to the currently resolved exact version.
	Version string `yaml:"version"`
	Source  string `yaml:"source"`
	// Role defines how Lucy should treat this package in desired state.
	//
	// - required: explicit operator intent, including user-selected leaf nodes during adopt
	// - transitive: resolver-derived dependency Lucy may auto-prune when no longer needed
	// - ignored: known content Lucy sees but must leave outside sync responsibility
	//
	// Non-leaf nodes remain visible to init/adopt users because Minecraft package
	// boundaries are often fuzzy, but that visibility must not become a fourth role.
	Role     ManifestRole `yaml:"role"`
	Side     ManifestSide `yaml:"side"`
	Optional bool         `yaml:"optional"`
	Pinned   bool         `yaml:"pinned"`
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
	Name     string     `yaml:"name"`
	Type     BundleType `yaml:"type"`
	Path     string     `yaml:"path"`
	Source   string     `yaml:"source"`
	Optional bool       `yaml:"optional"`
}

func ManifestDefaults() Manifest {
	return Manifest{
		Environment: ManifestEnvironment{
			GameVersion:            "",
			ServerCore:             "",
			ServerCoreVersion:      "",
			ModdingPlatform:        "",
			ModdingPlatformVersion: "",
			CompatiblePlatforms:    []string{},
			Mcdr:                   false,
			DeclaredCapabilities:   []string{},
		},
		Packages: []ManifestPackage{},
		Bundles:  []ManifestBundle{},
	}
}

func ValidateManifest(m Manifest) error {
	if err := ValidateManifestEnvironment(m.Environment); err != nil {
		return err
	}

	for i, pkg := range m.Packages {
		if err := validateManifestPackage(pkg); err != nil {
			return malformedStateError(
				ManifestFile,
				fmt.Sprintf("packages[%d]", i),
				err,
			)
		}
	}

	for i, bundle := range m.Bundles {
		if err := validateManifestBundle(bundle); err != nil {
			return malformedStateError(
				ManifestFile,
				fmt.Sprintf("bundles[%d]", i),
				err,
			)
		}
	}

	if m.Config != nil {
		if err := ValidateConfig(*m.Config); err != nil {
			return err
		}
	}

	return nil
}

func ValidateManifestEnvironment(env ManifestEnvironment) error {
	platform := strings.TrimSpace(env.ModdingPlatform)
	version := strings.TrimSpace(env.ModdingPlatformVersion)

	if platform == "" {
		if version != "" {
			return NewStateError(
				ManifestFile,
				ErrMalformed,
				"environment.modding_platform_version",
				"environment.modding_platform_version requires environment.modding_platform",
			)
		}
		if len(env.CompatiblePlatforms) > 0 {
			return NewStateError(
				ManifestFile,
				ErrMalformed,
				"environment.compatible_platforms",
				"environment.compatible_platforms requires environment.modding_platform",
			)
		}
		return nil
	}

	switch platform {
	case "bare", "none", "fabric", "forge", "neoforge", "mcdr":
	default:
		return NewStateError(
			ManifestFile,
			ErrMalformed,
			"environment.modding_platform",
			fmt.Sprintf(
				"invalid environment.modding_platform %q",
				env.ModdingPlatform,
			),
		)
	}

	if (platform == "bare" || platform == "none") && len(env.CompatiblePlatforms) > 0 {
		return NewStateError(
			ManifestFile,
			ErrMalformed,
			"environment.compatible_platforms",
			"environment.compatible_platforms requires a non-vanilla environment.modding_platform",
		)
	}

	seen := make(map[string]struct{}, len(env.CompatiblePlatforms))
	for i, raw := range env.CompatiblePlatforms {
		value := strings.TrimSpace(raw)
		if value == "" {
			return NewStateError(
				ManifestFile,
				ErrMalformed,
				fmt.Sprintf("environment.compatible_platforms[%d]", i),
				"environment.compatible_platforms entries must be non-empty",
			)
		}
		switch value {
		case "fabric", "forge", "neoforge", "mcdr", "sinytra":
		default:
			return NewStateError(
				ManifestFile,
				ErrMalformed,
				fmt.Sprintf("environment.compatible_platforms[%d]", i),
				fmt.Sprintf("invalid compatible platform %q", raw),
			)
		}
		if value == platform {
			return NewStateError(
				ManifestFile,
				ErrMalformed,
				fmt.Sprintf("environment.compatible_platforms[%d]", i),
				fmt.Sprintf(
					"compatible platform %q duplicates environment.modding_platform",
					raw,
				),
			)
		}
		if _, ok := seen[value]; ok {
			return NewStateError(
				ManifestFile,
				ErrMalformed,
				fmt.Sprintf("environment.compatible_platforms[%d]", i),
				fmt.Sprintf("duplicate compatible platform %q", raw),
			)
		}
		seen[value] = struct{}{}
	}

	if _, hasSinytra := seen["sinytra"]; hasSinytra && platform != "neoforge" {
		return NewStateError(
			ManifestFile,
			ErrMalformed,
			"environment.compatible_platforms",
			"environment.compatible_platforms cannot include \"sinytra\" unless environment.modding_platform is \"neoforge\"",
		)
	}

	return nil
}

// validateManifestEcosystem remains as a legacy helper for the pre-Task-2 lock
// schema, which still validates a single platform field.
func validateManifestEcosystem(value string) error {
	platform := types.Ecosystem(strings.TrimSpace(value))
	if platform == "" {
		return fmt.Errorf("environment.platform is required")
	}

	switch platform {
	case types.EcoFabric, types.EcoNeoforge, types.EcoForge, types.EcoMcdr, types.EcoUnspecified:
		return nil
	default:
		return fmt.Errorf("invalid environment.platform %q", value)
	}
}

func validateManifestPackage(pkg ManifestPackage) error {
	if strings.TrimSpace(pkg.ID) == "" {
		return fmt.Errorf("id is required")
	}
	if strings.Contains(pkg.ID, "/") {
		return fmt.Errorf("id must be a package name, not platform/name")
	}

	if strings.TrimSpace(pkg.Version) == "" {
		return fmt.Errorf("version is required")
	}
	version := types.BareVersion(pkg.Version)
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
		return fmt.Errorf(
			"invalid role %q; expected one of required, transitive, ignored",
			pkg.Role,
		)
	}

	switch pkg.Side {
	case SideServer, SideClient, SideBoth, SideUnknown:
	default:
		return fmt.Errorf("invalid side %q", pkg.Side)
	}

	return nil
}

func CompatiblePlatformOptions(primary string) []string {
	switch strings.TrimSpace(primary) {
	case "neoforge":
		return []string{"fabric", "mcdr", "sinytra"}
	case "fabric", "forge":
		return []string{"mcdr"}
	default:
		return nil
	}
}

func NormalizeManifestVersionIntent(version types.BareVersion) string {
	trimmed := strings.TrimSpace(version.String())
	switch trimmed {
	case "", "any", "none", "unknown":
		return types.VersionAny.String()
	default:
		return trimmed
	}
}

func UpsertManifestRequiredIntent(
	manifest *Manifest,
	req types.PackageRequest,
	source string,
) *Manifest {
	if manifest == nil {
		manifest = new(ManifestDefaults())
	} else {
		clone := *manifest
		clone.Environment.CompatiblePlatforms = append(
			[]string(nil),
			manifest.Environment.CompatiblePlatforms...,
		)
		clone.Environment.DeclaredCapabilities = append(
			[]string(nil),
			manifest.Environment.DeclaredCapabilities...,
		)
		clone.Packages = append([]ManifestPackage(nil), manifest.Packages...)
		clone.Bundles = append([]ManifestBundle(nil), manifest.Bundles...)
		manifest = &clone
	}

	resolvedSource := strings.TrimSpace(source)
	if types.ParseSource(resolvedSource) == types.SourceUnknown {
		resolvedSource = "auto"
	}
	intentVersion := NormalizeManifestVersionIntent(req.Version)
	refID := req.Name.String()

	for i := range manifest.Packages {
		if manifest.Packages[i].ID != refID {
			continue
		}
		manifest.Packages[i].Version = intentVersion
		manifest.Packages[i].Source = resolvedSource
		manifest.Packages[i].Role = RoleRequired
		manifest.Packages[i].Optional = false
		if manifest.Packages[i].Side == "" {
			manifest.Packages[i].Side = SideUnknown
		}
		sort.Slice(
			manifest.Packages, func(i, j int) bool {
				return manifest.Packages[i].ID < manifest.Packages[j].ID
			},
		)
		return manifest
	}

	manifest.Packages = append(
		manifest.Packages, ManifestPackage{
			ID:      refID,
			Version: intentVersion,
			Source:  resolvedSource,
			Role:    RoleRequired,
			Side:    SideUnknown,
		},
	)
	sort.Slice(
		manifest.Packages, func(i, j int) bool {
			return manifest.Packages[i].ID < manifest.Packages[j].ID
		},
	)
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
			version = types.VersionAny.String()
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
		packages = append(
			packages, ManifestPackage{
				ID:       id,
				Version:  version,
				Source:   source,
				Role:     role,
				Side:     side,
				Optional: pkg.Optional,
				Pinned:   pkg.Pinned,
			},
		)
	}
	sort.Slice(
		packages, func(i, j int) bool {
			return packages[i].ID < packages[j].ID
		},
	)
	return packages
}

func UpdateManifestRolesForAdd(
	manifest *Manifest,
	requested []types.PackageRequest,
	lock *Lock,
) *Manifest {
	base := cloneManifestOrDefaults(manifest)
	required := manifestPackagesByRole(base.Packages, RoleRequired)
	ignored := manifestPackagesByRole(base.Packages, RoleIgnored)

	for _, req := range requested {
		resolvedID := resolveManifestPackageID(req.PackageRef, &base, lock)
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
		pkg.Version = requestedManifestVersion(req.Version, pkg.Version)
		pkg.Source = normalizedManifestSource(req.Source.String())
		if pkg.Side == "" {
			pkg.Side = SideUnknown
		}
		pkg.Optional = false
		required[resolvedID] = pkg
	}

	base.Packages = rebuildManifestPackages(required, ignored, lock)
	return &base
}

func UpdateManifestRolesForRemove(
	manifest *Manifest,
	removed []types.VersionedPackageRef,
	lock *Lock,
) *Manifest {
	base := cloneManifestOrDefaults(manifest)
	required := manifestPackagesByRole(base.Packages, RoleRequired)
	ignored := manifestPackagesByRole(base.Packages, RoleIgnored)

	for _, id := range removed {
		resolvedID := resolveManifestPackageID(id.PackageRef, &base, lock)
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
	cloned.Environment.CompatiblePlatforms = append(
		[]string(nil),
		manifest.Environment.CompatiblePlatforms...,
	)
	cloned.Environment.DeclaredCapabilities = append(
		[]string(nil),
		manifest.Environment.DeclaredCapabilities...,
	)
	cloned.Packages = append([]ManifestPackage(nil), manifest.Packages...)
	cloned.Bundles = append([]ManifestBundle(nil), manifest.Bundles...)
	normalizeManifest(&cloned)
	return cloned
}

func manifestPackagesByRole(
	packages []ManifestPackage,
	role ManifestRole,
) map[string]ManifestPackage {
	indexed := make(map[string]ManifestPackage)
	for _, pkg := range packages {
		if pkg.Role != role {
			continue
		}
		indexed[pkg.ID] = pkg
	}
	return indexed
}

func manifestPackageByID(
	packages []ManifestPackage,
	id string,
) (ManifestPackage, bool) {
	for _, pkg := range packages {
		if pkg.ID == id {
			return pkg, true
		}
	}
	return ManifestPackage{}, false
}

func rebuildManifestPackages(
	required map[string]ManifestPackage,
	ignored map[string]ManifestPackage,
	lock *Lock,
) []ManifestPackage {
	classified := make([]ClassifiedPackage, 0, len(required)+len(ignored))
	requiredIDs := make(map[string]struct{}, len(required))
	ignoredIDs := make(map[string]struct{}, len(ignored))

	for id, pkg := range required {
		requiredIDs[id] = struct{}{}
		classified = append(
			classified, ClassifiedPackage{
				ID:       id,
				Version:  pkg.Version,
				Source:   normalizedManifestSource(pkg.Source),
				Role:     RoleRequired,
				Side:     normalizedManifestSide(pkg.Side),
				Optional: pkg.Optional,
				Pinned:   pkg.Pinned,
			},
		)
	}
	for id, pkg := range ignored {
		ignoredIDs[id] = struct{}{}
		classified = append(
			classified, ClassifiedPackage{
				ID:       id,
				Version:  pkg.Version,
				Source:   normalizedManifestSource(pkg.Source),
				Role:     RoleIgnored,
				Side:     normalizedManifestSide(pkg.Side),
				Optional: pkg.Optional,
				Pinned:   pkg.Pinned,
			},
		)
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

			classified = append(
				classified, ClassifiedPackage{
					ID:       locked.ID,
					Version:  locked.Version,
					Source:   normalizedManifestSource(locked.Source),
					Role:     RoleTransitive,
					Side:     normalizedManifestSide(ManifestSide(locked.Side)),
					Optional: locked.Optional,
				},
			)
		}
	}

	return ManifestPackagesFromClassified(classified)
}

func lockedPackageReachableFromRequired(
	pkg LockedPackage,
	required map[string]struct{},
) bool {
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

func requestedManifestVersion(
	version types.BareVersion,
	fallback string,
) string {
	if version == types.VersionAny {
		if strings.TrimSpace(fallback) != "" {
			return fallback
		}
		return types.VersionAny.String()
	}
	return version.String()
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
		Version: types.VersionAny.String(),
		Source:  "auto",
		Role:    RoleRequired,
		Side:    SideUnknown,
	}
}

func resolveManifestPackageID(
	ref types.PackageRef,
	manifest *Manifest,
	lock *Lock,
) string {
	if ref.Name == "" {
		return ""
	}
	if ref.Source != types.SourceAuto && ref.Source != types.SourceUnknown {
		if manifest != nil {
			for _, pkg := range manifest.Packages {
				if pkg.ID == ref.Name.String() && types.ParseSource(pkg.Source) == ref.Source {
					return pkg.ID
				}
			}
		}
		if lock != nil {
			for _, pkg := range lock.Packages {
				if pkg.ID == ref.Name.String() && types.ParseSource(pkg.Source) == ref.Source {
					return pkg.ID
				}
			}
		}
		return ref.Name.String()
	}
	if manifest != nil {
		var match string
		for _, pkg := range manifest.Packages {
			if pkg.ID != ref.Name.String() {
				continue
			}
			if match != "" && match != pkg.ID {
				return ""
			}
			match = pkg.ID
		}
		if match != "" {
			return match
		}
	}
	return ref.Name.String()
}

func (m Manifest) Marshal() ([]byte, error) {
	return yaml.Marshal(m)
}

func (m *Manifest) Unmarshal(data []byte) error {
	if err := yaml.Unmarshal(data, m); err != nil {
		return err
	}
	normalizeManifest(m)
	return nil
}

func normalizeManifest(m *Manifest) {
	if m == nil {
		return
	}
	if m.Environment.DeclaredCapabilities == nil {
		m.Environment.DeclaredCapabilities = []string{}
	}
	if m.Environment.CompatiblePlatforms == nil {
		m.Environment.CompatiblePlatforms = []string{}
	}
	m.Packages = normalizeManifestPackages(m.Packages)
	if m.Bundles == nil {
		m.Bundles = []ManifestBundle{}
	}
}

func normalizeManifestPackages(packages []ManifestPackage) []ManifestPackage {
	if packages == nil {
		return []ManifestPackage{}
	}

	compacted := packages[:0]
	for _, pkg := range packages {
		if isBlankManifestPackage(pkg) {
			continue
		}
		compacted = append(compacted, pkg)
	}
	return compacted
}

func isBlankManifestPackage(pkg ManifestPackage) bool {
	return strings.TrimSpace(pkg.ID) == "" &&
		strings.TrimSpace(pkg.Version) == "" &&
		strings.TrimSpace(pkg.Source) == "" &&
		pkg.Role == "" &&
		pkg.Side == "" &&
		!pkg.Optional &&
		!pkg.Pinned
}
