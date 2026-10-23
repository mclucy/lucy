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
	SourceUnknown  SelectionSource = ""
	SourceInferred SelectionSource = "inferred"
	SourceDeclared SelectionSource = "declared"
	SourceExplicit SelectionSource = "explicit"
)

// Declaration is the applicable persisted server intent. EntryPoint is an
// absolute path built by the workspace from the locked relative entry point.
type Declaration struct {
	Product     server.Product
	Minecraft   versionpkg.Version
	CoreVersion versionpkg.Version
	EntryPoint  string
}

// Request is one selection attempt. A nonempty explicit EntryPoint overrides
// the declaration.
type Request struct {
	EntryPoint string
	Declared   *Declaration
}

// Selection is the read-only result of a selection attempt. It references the
// chosen installation and one actual entry-point member.
type Selection struct {
	State         SelectionState
	Source        SelectionSource
	Installation  *Installation
	EntryPoint    *File
	RequestedPath string
	Declared      *Declaration
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
		Declared:      request.Declared,
	}
	selectable := r.selectableInstallations()

	if request.EntryPoint != "" {
		return r.selectExplicit(request.EntryPoint, selectable)
	}
	if request.Declared != nil {
		return r.selectDeclared(request.Declared, selectable)
	}
	return r.selectInferred(selectable)
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
	if declaration.EntryPoint != "" {
		withPath := installationsMatchingDeclaredPath(selectable, declaration)
		switch len(withPath) {
		case 1:
			file := entryPointAt(withPath[0], declaration.EntryPoint)
			if file != nil {
				r.selectInstallation(withPath[0], file, SourceDeclared)
				return nil
			}
		case 0:
			if len(matches) == 1 {
				if managed := managedEntryPoint(matches[0]); managed != nil {
					r.selectInstallation(matches[0], managed, SourceDeclared)
					return nil
				}
			}
		default:
			r.Selection.State = SelectionAmbiguous
			return &SelectionError{
				State:         SelectionAmbiguous,
				Path:          declaration.EntryPoint,
				Installations: cloneInstallations(withPath),
			}
		}
		r.Selection.State = SelectionMissingDeclared
		return &SelectionError{
			State:         SelectionMissingDeclared,
			Path:          declaration.EntryPoint,
			Installations: cloneInstallations(selectable),
		}
	}

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
			if !concreteVersion(observed) || observed != declaration.Minecraft {
				continue
			}
		}
		if concreteVersion(declaration.CoreVersion) {
			observed := installation.Identity.Primary.Version
			if !concreteVersion(observed) || observed != declaration.CoreVersion {
				continue
			}
		}
		matches = append(matches, installation)
	}
	return matches
}

// installationsMatchingDeclaredPath finds the installation that owns the
// declared entry point. The path itself is authoritative; observed concrete
// versions must not contradict the declaration, while unknown observed
// versions cannot disprove it.
func installationsMatchingDeclaredPath(
	installations []*Installation,
	declaration *Declaration,
) []*Installation {
	if declaration == nil || declaration.EntryPoint == "" {
		return nil
	}
	matches := make([]*Installation, 0, 1)
	for _, installation := range installations {
		if !installation.Identity.IsValid() ||
			entryPointAt(installation, declaration.EntryPoint) == nil {
			continue
		}
		if !installationMatchesProduct(installation, declaration.Product) {
			continue
		}
		observedMinecraft := installation.Identity.MinecraftVersion()
		if concreteVersion(declaration.Minecraft) &&
			concreteVersion(observedMinecraft) &&
			observedMinecraft != declaration.Minecraft {
			continue
		}
		observedCore := installation.Identity.Primary.Version
		if concreteVersion(declaration.CoreVersion) &&
			concreteVersion(observedCore) &&
			observedCore != declaration.CoreVersion {
			continue
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

func managedEntryPoint(installation *Installation) *File {
	if installation == nil {
		return nil
	}
	var chosen *File
	for file := range installation.EntryPoints() {
		if file.Ownership != OwnershipManaged {
			continue
		}
		if chosen == nil || file.Path < chosen.Path {
			chosen = file
		}
	}
	return chosen
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
