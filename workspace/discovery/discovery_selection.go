package discovery

import (
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
	SelectionUnmatched       SelectionState = "unmatched"
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

// Expectation narrows candidate installations by identity. Empty fields match
// any value; a concrete version must equal an observed concrete version.
// Expectation is an invocation filter, not a persisted declaration.
type Expectation struct {
	Product     server.Product
	Minecraft   versionpkg.Version
	CoreVersion versionpkg.Version
}

// Request is one selection attempt. Installation selects a result member
// directly. Expectation narrows the candidates and takes precedence over the
// persisted declaration because it describes this invocation. Policy
// PreferModified is opted into only by live status.
type Request struct {
	Installation *Installation
	Expectation  *Expectation
	Declared     *Declaration
	Policy       SelectionPolicy
}

// Selection is the read-only result of a selection attempt. It references the
// chosen installation; member roles remain discovery evidence.
type Selection struct {
	State        SelectionState
	Source       SelectionSource
	Installation *Installation
}

// IsValid reports whether the selection names a launchable installation with a
// usable identity.
func (s Selection) IsValid() bool {
	return s.State == SelectionSelected &&
		launchableInstallation(s.Installation)
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
	r.Selection = Selection{State: SelectionNone}
	selectable := r.selectableInstallations()

	switch {
	case request.Installation != nil:
		return r.selectDirect(request.Installation, selectable)
	case request.Expectation != nil:
		return r.selectExpectation(request.Expectation, selectable)
	case request.Declared != nil:
		return r.selectDeclared(request.Declared, selectable)
	default:
		return r.selectInferred(request.Policy, selectable)
	}
}

// selectDirect applies a caller's direct installation choice. The installation
// must belong to this result and satisfy the launchability evidence.
func (r *Result) selectDirect(
	installation *Installation,
	selectable []*Installation,
) error {
	for _, candidate := range selectable {
		if candidate == installation {
			r.selectCandidate(candidate, SourceExplicit)
			return nil
		}
	}
	r.Selection.State = SelectionInvalid
	return &SelectionError{
		State:         SelectionInvalid,
		Installations: copyInstallations(r.Installations),
	}
}

// selectExpectation filters the launchable installations by identity. It never
// supplies a fact that discovery did not observe.
func (r *Result) selectExpectation(
	expectation *Expectation,
	selectable []*Installation,
) error {
	matches := installationsMatchingExpectation(selectable, expectation)
	switch len(matches) {
	case 1:
		r.selectCandidate(matches[0], SourceExplicit)
		return nil
	case 0:
		r.Selection.State = SelectionUnmatched
		return &SelectionError{
			State:         SelectionUnmatched,
			Installations: copyInstallations(r.Installations),
		}
	default:
		r.Selection.State = SelectionAmbiguous
		return &SelectionError{
			State:         SelectionAmbiguous,
			Installations: cloneInstallations(matches),
		}
	}
}

func (r *Result) selectDeclared(
	declaration *Declaration,
	selectable []*Installation,
) error {
	matches := installationsMatchingDeclaration(selectable, declaration)
	switch len(matches) {
	case 1:
		r.selectCandidate(matches[0], SourceDeclared)
		return nil
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
		r.selectCandidate(selectable[0], SourceInferred)
		return nil
	default:
		if policy == PolicyPreferModified {
			if preferred := preferredInstallation(selectable); preferred != nil {
				r.selectCandidate(preferred, SourcePreferred)
				return nil
			}
		}
	}
	r.Selection.State = SelectionAmbiguous
	return &SelectionError{
		State:         SelectionAmbiguous,
		Installations: cloneInstallations(selectable),
	}
}

func (r *Result) selectCandidate(
	installation *Installation,
	source SelectionSource,
) {
	r.Selection.State = SelectionSelected
	r.Selection.Source = source
	r.Selection.Installation = installation
}

func (r *Result) selectableInstallations() []*Installation {
	selectable := make([]*Installation, 0, len(r.Installations))
	for index := range r.Installations {
		installation := &r.Installations[index]
		if !launchableInstallation(installation) {
			continue
		}
		selectable = append(selectable, installation)
	}
	return selectable
}

// launchableInstallation reports whether discovery has layout evidence that the
// installation can start. Entry-point members prove launchability; they never
// enter Selection.
func launchableInstallation(installation *Installation) bool {
	if installation == nil || !installation.Identity.IsValid() ||
		firstEntryPoint(installation) == nil {
		return false
	}
	entry := chooseEntryPoint(installation)
	return entry != nil && entry.Ownership != OwnershipUnreadable
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

// installationsMatchingExpectation applies an invocation filter. Empty fields
// match any observed value; concrete expectation versions must equal an
// observed concrete version. Unknown observed metadata never proves a conflict.
func installationsMatchingExpectation(
	installations []*Installation,
	expectation *Expectation,
) []*Installation {
	if expectation == nil {
		return nil
	}
	matches := make([]*Installation, 0, len(installations))
	for _, installation := range installations {
		if !installation.Identity.IsValid() {
			continue
		}
		if expectation.Product != server.ProductUnknown &&
			!installationMatchesProduct(installation, expectation.Product) {
			continue
		}
		if concreteVersion(expectation.Minecraft) {
			observed := installation.Identity.MinecraftVersion()
			if concreteVersion(observed) && observed != expectation.Minecraft {
				continue
			}
		}
		if concreteVersion(expectation.CoreVersion) {
			observed := installation.Identity.Primary.Version
			if concreteVersion(observed) && observed != expectation.CoreVersion {
				continue
			}
		}
		matches = append(matches, installation)
	}
	return matches
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

// chooseEntryPoint prefers a receipt-managed entry point, then a locally
// modified owned entry point, then a readable unmanaged entry point, and
// finally any remaining unreadable entry point. Filesystem timestamps never participate.
func chooseEntryPoint(installation *Installation) *File {
	if installation == nil {
		return nil
	}
	var firstReadable, unreadable, modified, managed *File
	for file := range installation.EntryPoints() {
		if file.Ownership == OwnershipUnreadable {
			if unreadable == nil || file.Path < unreadable.Path {
				unreadable = file
			}
			continue
		}
		if firstReadable == nil || file.Path < firstReadable.Path {
			firstReadable = file
		}
		if file.Ownership == OwnershipModified &&
			(modified == nil || file.Path < modified.Path) {
			modified = file
		}
		if file.Ownership == OwnershipManaged &&
			(managed == nil || file.Path < managed.Path) {
			managed = file
		}
	}
	if managed != nil {
		return managed
	}
	if modified != nil {
		return modified
	}
	if firstReadable != nil {
		return firstReadable
	}
	return unreadable
}

func entryPointOwnershipRank(installation *Installation) int {
	best := 0
	for file := range installation.EntryPoints() {
		rank := 0
		switch file.Ownership {
		case OwnershipManaged:
			rank = 4
		case OwnershipModified:
			rank = 3
		case OwnershipUnreadable:
			rank = 2
		case OwnershipUnmanaged:
			rank = 1
		default:
			rank = 0
		}
		if rank > best {
			best = rank
		}
	}
	return best
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
	leftRank := entryPointOwnershipRank(left)
	rightRank := entryPointOwnershipRank(right)
	if leftRank != rightRank {
		return rightRank - leftRank
	}
	if result := strings.Compare(identityKey(left.Identity), identityKey(right.Identity)); result != 0 {
		return result
	}
	return strings.Compare(firstEntryPointPath(left), firstEntryPointPath(right))
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
