package discovery

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

// SelectionState distinguishes the explicit outcomes of a selection request.
type SelectionState string

const (
	SelectionUnknown         SelectionState = ""
	SelectionNone            SelectionState = "none"
	SelectionSelected        SelectionState = "selected"
	SelectionAmbiguous       SelectionState = "ambiguous"
	SelectionMissingDeclared SelectionState = "missing_declared"
	SelectionInvalid         SelectionState = "invalid"
)

// SelectionSource records why an installation was selected.
type SelectionSource string

const (
	SourceUnknown   SelectionSource = ""
	SourceInferred  SelectionSource = "inferred"
	SourceDeclared  SelectionSource = "declared"
	SourceExplicit  SelectionSource = "explicit"
	SourcePreferred SelectionSource = "preferred"
)

// SelectionPolicy controls whether an ambiguous observation may be resolved
// by the read-only preference order.
type SelectionPolicy string

const (
	PolicyUnknown        SelectionPolicy = ""
	PolicyStrict         SelectionPolicy = "strict"
	PolicyPreferModified SelectionPolicy = "prefer_modified"
)

// Declaration is the applicable persisted installation identity.
type Declaration struct {
	Product     server.Product
	Minecraft   versionpkg.Version
	CoreVersion versionpkg.Version
}

// Request is one selection attempt. A nonempty explicit EntryPoint overrides
// the declaration. PolicyPreferModified is opted into only by live status.
type Request struct {
	EntryPoint string
	Declared   *Declaration
	Policy     SelectionPolicy
}

// Selection is the read-only result of a selection attempt. It references the
// chosen installation and one actual entry-point member.
type Selection struct {
	State         SelectionState
	Source        SelectionSource
	Installation  *Installation
	EntryPoint    *File
	RequestedPath string
}

// IsValid reports whether the selection names a chosen installation and a
// real entry-point member with a usable identity.
func (s Selection) IsValid() bool {
	return s.State == SelectionSelected &&
		s.Installation != nil &&
		s.Installation.Identity.IsValid() &&
		s.EntryPoint != nil &&
		s.EntryPoint.Role == RoleEntryPoint &&
		s.EntryPoint.Path != ""
}

// Product returns the selected installation product.
func (s Selection) Product() server.Product {
	if !s.IsValid() {
		return server.ProductUnknown
	}
	return s.Installation.Identity.Product()
}

// MinecraftVersion returns the selected installation Minecraft version.
func (s Selection) MinecraftVersion() versionpkg.Version {
	if !s.IsValid() {
		return versionpkg.Unknown
	}
	return s.Installation.Identity.MinecraftVersion()
}

// Ecosystem returns the selected installation loader ecosystem.
func (s Selection) Ecosystem() catalog.Ecosystem {
	if !s.IsValid() {
		return catalog.EcosystemUnspecified
	}
	return s.Installation.Identity.Ecosystem()
}

// Select updates the result selection without mutating or reordering members.
// It never writes files. Every non-selected outcome returns *SelectionError.
func (r *Result) Select(request Request) error {
	if r == nil {
		return &SelectionError{State: SelectionNone}
	}
	r.Selection = Selection{
		State:         SelectionNone,
		RequestedPath: request.EntryPoint,
	}
	selectable := r.selectableInstallations()

	if request.EntryPoint != "" {
		return r.selectExplicit(request.EntryPoint, selectable)
	}
	if request.Declared != nil {
		return r.selectDeclared(request.Declared, selectable)
	}
	return r.selectInferred(request.Policy, selectable)
}

func (r *Result) selectExplicit(path string, selectable []*Installation) error {
	matches := installationsWithEntryPoint(selectable, path)
	switch len(matches) {
	case 1:
		file := entryPointAt(matches[0], path)
		if file == nil {
			break
		}
		r.selectInstallation(matches[0], file, SourceExplicit)
		return nil
	case 0:
	default:
		r.Selection.State = SelectionInvalid
		return &SelectionError{
			State:         SelectionInvalid,
			Path:          path,
			Installations: cloneInstallations(matches),
		}
	}
	r.Selection.State = SelectionInvalid
	return &SelectionError{
		State:         SelectionInvalid,
		Path:          path,
		Installations: cloneInstallations(selectable),
	}
}

func (r *Result) selectDeclared(
	declaration *Declaration,
	selectable []*Installation,
) error {
	matches := installationsMatchingDeclaration(selectable, declaration)
	switch len(matches) {
	case 1:
		if file := chooseEntryPoint(matches[0]); file != nil {
			r.selectInstallation(matches[0], file, SourceDeclared)
			return nil
		}
	case 0:
	default:
		r.Selection.State = SelectionAmbiguous
		return &SelectionError{
			State:         SelectionAmbiguous,
			Installations: cloneInstallations(matches),
		}
	}
	r.Selection.State = SelectionMissingDeclared
	return &SelectionError{
		State:         SelectionMissingDeclared,
		Installations: cloneInstallations(selectable),
	}
}

func (r *Result) selectInferred(
	policy SelectionPolicy,
	selectable []*Installation,
) error {
	switch len(selectable) {
	case 0:
		if len(r.Installations) > 0 {
			r.Selection.State = SelectionInvalid
			return &SelectionError{
				State:         SelectionInvalid,
				Installations: copyInstallations(r.Installations),
			}
		}
		r.Selection.State = SelectionNone
		return &SelectionError{State: SelectionNone}
	case 1:
		if file := chooseEntryPoint(selectable[0]); file != nil {
			r.selectInstallation(selectable[0], file, SourceInferred)
			return nil
		}
	default:
		if policy == PolicyPreferModified {
			if preferred := preferredInstallation(selectable); preferred != nil {
				if file := chooseEntryPoint(preferred); file != nil {
					r.selectInstallation(preferred, file, SourcePreferred)
					return nil
				}
			}
		}
	}
	r.Selection.State = SelectionAmbiguous
	return &SelectionError{
		State:         SelectionAmbiguous,
		Installations: cloneInstallations(selectable),
	}
}

func (r *Result) selectInstallation(
	installation *Installation,
	entryPoint *File,
	source SelectionSource,
) {
	r.Selection.State = SelectionSelected
	r.Selection.Source = source
	r.Selection.Installation = installation
	r.Selection.EntryPoint = entryPoint
}

func (r *Result) selectableInstallations() []*Installation {
	selectable := make([]*Installation, 0, len(r.Installations))
	for index := range r.Installations {
		installation := &r.Installations[index]
		if !selectableInstallation(installation) {
			continue
		}
		selectable = append(selectable, installation)
	}
	return selectable
}

// A known version conflict excludes an installation. Missing metadata does not
// establish a conflict or prove equality between candidates; selection still
// requires one matching installation.
func installationsMatchingDeclaration(
	installations []*Installation,
	declaration *Declaration,
) []*Installation {
	if declaration == nil || declaration.Product == server.ProductUnknown {
		return nil
	}
	matches := make([]*Installation, 0, len(installations))
	for _, installation := range installations {
		if !installation.Identity.IsValid() {
			continue
		}
		if !installationMatchesProduct(installation, declaration.Product) {
			continue
		}
		if concreteVersion(declaration.Minecraft) {
			observed := installation.Identity.MinecraftVersion()
			if concreteVersion(observed) && observed != declaration.Minecraft {
				continue
			}
		}
		if concreteVersion(declaration.CoreVersion) {
			observed := installation.Identity.Primary.Version
			if concreteVersion(observed) && observed != declaration.CoreVersion {
				continue
			}
		}
		matches = append(matches, installation)
	}
	return matches
}

// installationMatchesProduct matches the catalogued product first. Products
// outside the server catalog, such as a Paper fork named only by its primary
// identity, match by canonical primary name.
func installationMatchesProduct(
	installation *Installation,
	product server.Product,
) bool {
	if installation == nil || product == server.ProductUnknown {
		return false
	}
	if installation.Identity.Product() == product {
		return true
	}
	return strings.EqualFold(
		installation.Identity.Primary.Name.String(),
		string(product),
	)
}

func installationsWithEntryPoint(
	installations []*Installation,
	path string,
) []*Installation {
	matches := make([]*Installation, 0, 1)
	for _, installation := range installations {
		if entryPointAt(installation, path) == nil {
			continue
		}
		matches = append(matches, installation)
	}
	return matches
}

func entryPointAt(installation *Installation, path string) *File {
	if installation == nil {
		return nil
	}
	for file := range installation.EntryPoints() {
		if samePath(file.Path, path) {
			return file
		}
	}
	return nil
}

func firstEntryPoint(installation *Installation) *File {
	if installation == nil {
		return nil
	}
	for file := range installation.EntryPoints() {
		return file
	}
	return nil
}

// chooseEntryPoint prefers a receipt-managed entry point, then the lexical
// first entry point. Filesystem timestamps never participate.
func chooseEntryPoint(installation *Installation) *File {
	if installation == nil {
		return nil
	}
	var first, managed *File
	for file := range installation.EntryPoints() {
		if first == nil || file.Path < first.Path {
			first = file
		}
		if file.Ownership == OwnershipManaged &&
			(managed == nil || file.Path < managed.Path) {
			managed = file
		}
	}
	if managed != nil {
		return managed
	}
	return first
}

func hasManagedEntryPoint(installation *Installation) bool {
	for file := range installation.EntryPoints() {
		if file.Ownership == OwnershipManaged {
			return true
		}
	}
	return false
}

func preferredInstallation(candidates []*Installation) *Installation {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if preferInstallation(candidate, best) < 0 {
			best = candidate
		}
	}
	return best
}

func preferInstallation(left, right *Installation) int {
	leftTier, leftOrder := productRank(left.Identity)
	rightTier, rightOrder := productRank(right.Identity)
	if leftTier != rightTier {
		return leftTier - rightTier
	}
	if leftOrder != rightOrder {
		return leftOrder - rightOrder
	}
	leftManaged := hasManagedEntryPoint(left)
	rightManaged := hasManagedEntryPoint(right)
	if leftManaged != rightManaged {
		if leftManaged {
			return -1
		}
		return 1
	}
	if result := strings.Compare(identityKey(left.Identity), identityKey(right.Identity)); result != 0 {
		return result
	}
	return strings.Compare(firstEntryPointPath(left), firstEntryPointPath(right))
}

// productRank implements the fixed live-status preference order. Lower tiers
// are preferred. Unrecognized products and proxies rank below every recognized
// Minecraft server.
func productRank(identity Identity) (int, int) {
	name := strings.ToLower(strings.TrimSpace(identity.Primary.Name.String()))
	switch name {
	case "youer":
		return 0, 0
	case "arclight", "arclight-forge", "arclight-neoforge", "arclight-fabric":
		return 0, 1
	case "catserver":
		return 0, 2
	case "spongeneo":
		return 0, 3
	case "spongeforge":
		return 0, 4
	case "neoforge":
		return 1, 0
	case "forge":
		return 1, 1
	case "fabric":
		return 1, 2
	case "reaper":
		return 2, 0
	case "leaf":
		return 2, 1
	case "divine":
		return 2, 2
	case "pufferfish":
		return 2, 3
	case "purpur":
		return 2, 4
	case "folia":
		return 2, 5
	case "leaves":
		return 2, 6
	case "paper":
		return 3, 0
	case "spigot":
		return 3, 1
	case "craftbukkit", "bukkit":
		return 3, 2
	case "spongevanilla":
		return 3, 3
	case "minecraft", "vanilla", "mc":
		return 4, 0
	case "bungeecord", "velocity", "waterfall":
		return 5, 0
	}
	switch identity.Product() {
	case server.ProductYouer:
		return 0, 0
	case server.ProductArclight, server.ProductArclightForge,
		server.ProductArclightNeoForge, server.ProductArclightFabric:
		return 0, 1
	case server.ProductCatServer:
		return 0, 2
	case server.ProductSpongeNeo:
		return 0, 3
	case server.ProductSpongeForge:
		return 0, 4
	case server.ProductNeoForge:
		return 1, 0
	case server.ProductForge:
		return 1, 1
	case server.ProductFabric:
		return 1, 2
	case server.ProductPurpur:
		return 2, 4
	case server.ProductFolia:
		return 2, 5
	case server.ProductLeaves:
		return 2, 6
	case server.ProductPaper:
		return 3, 0
	case server.ProductSpigot:
		return 3, 1
	case server.ProductCraftBukkit:
		return 3, 2
	case server.ProductSpongeVanilla:
		return 3, 3
	case server.ProductMinecraft:
		return 4, 0
	case server.ProductBungeeCord, server.ProductVelocity, server.ProductWaterfall:
		return 5, 0
	default:
		return 5, 1
	}
}

func firstEntryPointPath(installation *Installation) string {
	if installation == nil {
		return ""
	}
	path := ""
	for file := range installation.EntryPoints() {
		if path == "" || file.Path < path {
			path = file.Path
		}
	}
	return path
}

func cloneInstallations(installations []*Installation) []Installation {
	cloned := make([]Installation, 0, len(installations))
	for _, installation := range installations {
		if installation == nil {
			continue
		}
		cloned = append(cloned, *installation)
	}
	return cloned
}

func copyInstallations(installations []Installation) []Installation {
	return slices.Clone(installations)
}

// samePath compares canonical paths, resolving symlinks and identity when the
// filesystem can prove they name the same object.
func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	leftResolved, leftErr := filepath.EvalSymlinks(left)
	rightResolved, rightErr := filepath.EvalSymlinks(right)
	return leftErr == nil && rightErr == nil &&
		filepath.Clean(leftResolved) == filepath.Clean(rightResolved)
}
