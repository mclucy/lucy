package discovery

import (
	"errors"
	"fmt"
	"iter"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

// FileRole describes how a member participates in an installation.
type FileRole string

const (
	RoleUnknown       FileRole = ""
	RoleEntryPoint    FileRole = "entry_point"
	RolePayload       FileRole = "payload"
	RoleLibrary       FileRole = "library"
	RoleConfiguration FileRole = "configuration"
)

// Ownership describes whether a member is tracked by the local installation
// receipt and whether its content still matches the recorded hash.
type Ownership string

const (
	OwnershipUnknown   Ownership = ""
	OwnershipUnmanaged Ownership = "unmanaged"
	OwnershipManaged   Ownership = "managed"
	OwnershipModified  Ownership = "modified"
)

// File is one member of an installation. It is data, not an open handle.
// Path is absolute and canonical.
type File struct {
	Path      string
	Role      FileRole
	Ownership Ownership
}

// Identity is the observed runtime identity of an installation. Primary is a
// value; Components carry the additional runtime components that detectors
// proved. Normalize canonicalizes both in place before any comparison.
type Identity struct {
	Primary    catalog.VersionedRef
	Components []catalog.VersionedRef
}

// Normalize canonicalizes names and ecosystems, removes exact duplicates, and
// upgrades an unknown component version to a concrete one when both occur for
// the same base identity. Conflicting concrete versions for one base identity
// are kept and reported as an error so the caller can emit a typed diagnostic;
// they are never silently erased.
func (i *Identity) Normalize() error {
	if i == nil {
		return errors.New("discovery: nil identity")
	}

	var problems []error
	primary, ok := canonicalPrimary(i.Primary)
	if !ok {
		problems = append(problems, fmt.Errorf(
			"invalid primary runtime %q",
			i.Primary.StringQualified(),
		))
	} else {
		i.Primary = primary
	}
	primaryKey := ""
	if ok {
		primaryKey = componentKey(i.Primary)
	}

	components := make([]catalog.VersionedRef, 0, len(i.Components))
	indexes := make(map[string]int, len(i.Components))
	seen := make(map[string]bool, len(i.Components))
	for _, raw := range i.Components {
		component, ok := canonicalComponent(raw)
		if !ok {
			problems = append(problems, fmt.Errorf(
				"invalid runtime component %q",
				raw.StringQualified(),
			))
			continue
		}
		if primaryKey != "" && componentKey(component) == primaryKey &&
			!concreteVersion(i.Primary.Version) &&
			concreteVersion(component.Version) {
			// The component carries exact evidence for the same base
			// identity; project it onto the primary value.
			i.Primary.Version = component.Version
		}
		key := componentKey(component)
		full := key + "@" + component.Version.String()
		if seen[full] {
			continue
		}
		seen[full] = true

		index, exists := indexes[key]
		if !exists {
			indexes[key] = len(components)
			components = append(components, component)
			continue
		}
		existing := components[index]
		switch {
		case existing.Version == component.Version:
			continue
		case !concreteVersion(existing.Version) && concreteVersion(component.Version):
			components[index] = component
		case concreteVersion(existing.Version) && !concreteVersion(component.Version):
			continue
		default:
			// Both versions are concrete and different. Keep both so the
			// contradiction remains observable.
			components = append(components, component)
		}
	}

	slices.SortFunc(components, compareComponent)
	i.Components = components
	if ok && i.Primary.Ecosystem == catalog.EcosystemMinecraft {
		i.Components = ensureMinecraftComponent(i.Components, i.Primary)
	}
	if err := i.contradiction(); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

// IsValid reports whether the identity has a canonical primary and only
// canonical, non-contradictory components. Unknown versions remain valid.
func (i Identity) IsValid() bool {
	primary, ok := canonicalPrimary(i.Primary)
	if !ok || primary != i.Primary {
		return false
	}
	if i.contradiction() != nil {
		return false
	}
	for _, component := range i.Components {
		canonical, ok := canonicalComponent(component)
		if !ok || canonical != component {
			return false
		}
	}
	return true
}

// IsConcrete reports whether every version the identity carries is an exact
// observed version. Only concrete identities may prove equality for merging.
func (i Identity) IsConcrete() bool {
	if !i.IsValid() || !concreteVersion(i.Primary.Version) {
		return false
	}
	for _, component := range i.Components {
		if redundantPrimaryComponent(i.Primary, component) {
			continue
		}
		if !concreteVersion(component.Version) {
			return false
		}
	}
	return true
}

// Equal compares the canonical complete structure of two identities. A
// component that merely repeats the primary identity adds no structure and is
// ignored.
func (i Identity) Equal(other Identity) bool {
	return identityKey(i) == identityKey(other)
}

// Product returns the executable server product named by the primary runtime.
func (i Identity) Product() server.Product {
	if i.Primary.Name == "" {
		return server.ProductUnknown
	}
	match, ok := server.NormalizeProduct(i.Primary)
	if !ok {
		return server.ProductUnknown
	}
	return match.Product
}

// MinecraftVersion returns the concrete Minecraft version carried by the
// identity, or version.Unknown when no single concrete version is known.
func (i Identity) MinecraftVersion() versionpkg.Version {
	if i.contradictsKey(componentKey(catalog.VersionedRef{
		Ecosystem: catalog.EcosystemMinecraft,
		Name:      "minecraft",
	})) {
		return versionpkg.Unknown
	}
	for _, component := range i.Components {
		if component.Ecosystem != catalog.EcosystemMinecraft ||
			component.Name != "minecraft" {
			continue
		}
		if concreteVersion(component.Version) {
			return component.Version
		}
	}
	if i.Primary.Ecosystem == catalog.EcosystemMinecraft &&
		concreteVersion(i.Primary.Version) {
		return i.Primary.Version
	}
	return versionpkg.Unknown
}

// Ecosystem reports the mod-loader ecosystem of the identity. It falls back to
// Minecraft when no loader component applies.
func (i Identity) Ecosystem() catalog.Ecosystem {
	if !i.IsValid() {
		return catalog.EcosystemUnspecified
	}
	name := strings.ToLower(i.Primary.Name.String())
	switch {
	case name == "fabric" || name == "fabric-loader":
		if i.Primary.Ecosystem == catalog.EcosystemFabric ||
			i.Primary.Ecosystem == catalog.EcosystemUnspecified {
			return catalog.EcosystemFabric
		}
	case name == "forge":
		if i.Primary.Ecosystem == catalog.EcosystemForge ||
			i.Primary.Ecosystem == catalog.EcosystemUnspecified {
			return catalog.EcosystemForge
		}
	case name == "neoforge":
		if i.Primary.Ecosystem == catalog.EcosystemNeoForge ||
			i.Primary.Ecosystem == catalog.EcosystemUnspecified {
			return catalog.EcosystemNeoForge
		}
	}
	for _, component := range i.Components {
		name := strings.ToLower(component.Name.String())
		switch {
		case component.Ecosystem == catalog.EcosystemFabric &&
			(name == "fabric-loader" || name == "fabricloader"):
			return catalog.EcosystemFabric
		case component.Ecosystem == catalog.EcosystemForge && name == "forge":
			return catalog.EcosystemForge
		case component.Ecosystem == catalog.EcosystemNeoForge && name == "neoforge":
			return catalog.EcosystemNeoForge
		}
	}
	return catalog.EcosystemMinecraft
}

func (i Identity) canonical() Identity {
	copied := Identity{
		Primary:    i.Primary,
		Components: slices.Clone(i.Components),
	}
	_ = copied.Normalize()
	return copied
}

func (i Identity) contradiction() error {
	versions := make(map[string]map[versionpkg.Version]struct{})
	record := func(ref catalog.VersionedRef) {
		if !concreteVersion(ref.Version) {
			return
		}
		key := componentKey(ref)
		if versions[key] == nil {
			versions[key] = make(map[versionpkg.Version]struct{})
		}
		versions[key][ref.Version] = struct{}{}
	}
	if primary, ok := canonicalPrimary(i.Primary); ok {
		record(primary)
	}
	for _, component := range i.Components {
		record(component)
	}
	var problems []error
	keys := make([]string, 0, len(versions))
	for key := range versions {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		found := versions[key]
		if len(found) <= 1 {
			continue
		}
		concrete := make([]versionpkg.Version, 0, len(found))
		for value := range found {
			concrete = append(concrete, value)
		}
		slices.Sort(concrete)
		problems = append(problems, fmt.Errorf(
			"conflicting concrete versions for %s: %s",
			key,
			strings.Join(versionStrings(concrete), " and "),
		))
	}
	return errors.Join(problems...)
}

func (i Identity) contradictsKey(key string) bool {
	versions := make(map[versionpkg.Version]struct{})
	if primary, ok := canonicalPrimary(i.Primary); ok &&
		componentKey(primary) == key &&
		concreteVersion(primary.Version) {
		versions[primary.Version] = struct{}{}
	}
	for _, component := range i.Components {
		if componentKey(component) == key && concreteVersion(component.Version) {
			versions[component.Version] = struct{}{}
		}
	}
	return len(versions) > 1
}

func canonicalPrimary(ref catalog.VersionedRef) (catalog.VersionedRef, bool) {
	name := strings.ToLower(strings.TrimSpace(ref.Name.String()))
	if ref.Version == "" {
		ref.Version = versionpkg.Unknown
	}
	if name == "" || !ref.Ecosystem.Valid() ||
		(!concreteVersion(ref.Version) && ref.Version != versionpkg.Unknown) {
		return catalog.VersionedRef{}, false
	}
	ref.Provider = catalog.ProviderUnspecified
	ref.Name = catalog.Name(name)
	return ref, true
}

func canonicalComponent(ref catalog.VersionedRef) (catalog.VersionedRef, bool) {
	name := strings.ToLower(strings.TrimSpace(ref.Name.String()))
	if ref.Version == "" {
		ref.Version = versionpkg.Unknown
	}
	if name == "" || (!concreteVersion(ref.Version) && ref.Version != versionpkg.Unknown) {
		return catalog.VersionedRef{}, false
	}
	switch ref.Ecosystem {
	case catalog.EcosystemMinecraft:
		if name != "minecraft" {
			return catalog.VersionedRef{}, false
		}
		name = "minecraft"
	case catalog.EcosystemFabric:
		switch name {
		case "fabric", "fabric-loader", "fabricloader":
			name = "fabric-loader"
		case "fabric-api":
			name = "fabric-api"
		default:
			return catalog.VersionedRef{}, false
		}
	case catalog.EcosystemForge:
		if name != "forge" {
			return catalog.VersionedRef{}, false
		}
	case catalog.EcosystemNeoForge:
		if name != "neoforge" {
			return catalog.VersionedRef{}, false
		}
	default:
		return catalog.VersionedRef{}, false
	}
	ref.Provider = catalog.ProviderUnspecified
	ref.Name = catalog.Name(name)
	return ref, true
}

func componentKey(ref catalog.VersionedRef) string {
	return ref.Ecosystem.String() + "/" + ref.Name.String()
}

func ensureMinecraftComponent(
	components []catalog.VersionedRef,
	primary catalog.VersionedRef,
) []catalog.VersionedRef {
	for index := range components {
		if components[index].Ecosystem != catalog.EcosystemMinecraft ||
			components[index].Name != "minecraft" {
			continue
		}
		if !concreteVersion(components[index].Version) &&
			concreteVersion(primary.Version) {
			components[index].Version = primary.Version
			slices.SortFunc(components, compareComponent)
		}
		return components
	}
	return append(components, catalog.VersionedRef{
		Ecosystem: catalog.EcosystemMinecraft,
		Name:      "minecraft",
		Version:   primary.Version,
	})
}

func compareComponent(left, right catalog.VersionedRef) int {
	if result := strings.Compare(left.Ecosystem.String(), right.Ecosystem.String()); result != 0 {
		return result
	}
	if result := strings.Compare(left.Name.String(), right.Name.String()); result != 0 {
		return result
	}
	return strings.Compare(left.Version.String(), right.Version.String())
}

func versionStrings(versions []versionpkg.Version) []string {
	out := make([]string, 0, len(versions))
	for _, value := range versions {
		out = append(out, value.String())
	}
	return out
}

func concreteVersion(value versionpkg.Version) bool {
	switch value {
	case "", versionpkg.None, versionpkg.Unknown, versionpkg.Any,
		versionpkg.Stable, versionpkg.Beta:
		return false
	default:
		return true
	}
}

// Installation is one observed runtime together with every member the
// recognizer proved belongs to it. All entry points, payloads, libraries, and
// configuration sidecars live in the single Files collection.
type Installation struct {
	Identity Identity
	Files    []File
}

// NewInstallation creates an unmanaged entry-point installation. Central
// reconciliation normalizes the identity.
func NewInstallation(identity Identity, entryPoint string) Installation {
	installation := Installation{Identity: identity}
	if entryPoint != "" {
		installation.AddFile(entryPoint, RoleEntryPoint)
	}
	return installation
}

// AddFile unions a member by path. A dependency member upgrades to an entry
// point when the same identity explicitly launches it; an existing entry point
// never downgrades.
func (i *Installation) AddFile(path string, role FileRole) {
	i.addFile(File{
		Path:      path,
		Role:      role,
		Ownership: OwnershipUnmanaged,
	})
}

// EntryPoints iterates the entry-point members of the single Files collection.
func (i *Installation) EntryPoints() iter.Seq[*File] {
	return func(yield func(*File) bool) {
		if i == nil {
			return
		}
		for index := range i.Files {
			if i.Files[index].Role != RoleEntryPoint {
				continue
			}
			if !yield(&i.Files[index]) {
				return
			}
		}
	}
}

func (i *Installation) addFile(file File) {
	if i == nil || file.Path == "" {
		return
	}
	file.Path = filepath.Clean(file.Path)
	if file.Ownership == OwnershipUnknown {
		file.Ownership = OwnershipUnmanaged
	}
	for index := range i.Files {
		existing := &i.Files[index]
		if existing.Path != file.Path {
			continue
		}
		if file.Role == RoleEntryPoint {
			existing.Role = RoleEntryPoint
		}
		existing.Ownership = strongerOwnership(existing.Ownership, file.Ownership)
		return
	}
	i.Files = append(i.Files, file)
}

func (i *Installation) removeFile(path string) {
	if i == nil {
		return
	}
	path = filepath.Clean(path)
	files := i.Files[:0]
	for _, file := range i.Files {
		if file.Path == path {
			continue
		}
		files = append(files, file)
	}
	clear(i.Files[len(files):])
	i.Files = files
}

func strongerOwnership(left, right Ownership) Ownership {
	rank := func(value Ownership) int {
		switch value {
		case OwnershipManaged:
			return 3
		case OwnershipModified:
			return 2
		case OwnershipUnmanaged:
			return 1
		default:
			return 0
		}
	}
	if rank(left) >= rank(right) {
		return left
	}
	return right
}

// identityKey is the stable canonical key used for ordering and grouping.
func identityKey(identity Identity) string {
	canonical := identity.canonical()
	primaryKey := componentKey(canonical.Primary)
	parts := make([]string, 0, 1+len(canonical.Components))
	parts = append(parts, primaryKey+"@"+canonical.Primary.Version.String())
	for _, component := range canonical.Components {
		if redundantPrimaryComponent(canonical.Primary, component) {
			continue
		}
		parts = append(parts, componentKey(component)+"@"+component.Version.String())
	}
	return strings.Join(parts, "|")
}

func redundantPrimaryComponent(
	primary catalog.VersionedRef,
	component catalog.VersionedRef,
) bool {
	if componentKey(primary) != componentKey(component) {
		return false
	}
	if component.Version == primary.Version {
		return true
	}
	return !concreteVersion(component.Version) && concreteVersion(primary.Version)
}

func compareIdentities(left, right Identity) int {
	return strings.Compare(identityKey(left), identityKey(right))
}
