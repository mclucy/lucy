package install

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/server"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/filetrack"
	versionpkg "github.com/mclucy/lucy/version"
)

// ResultSchema versions the machine-readable result.
const ResultSchema = 1

// ResultKind identifies the machine-readable install result document.
const ResultKind = "install"

// ResultKindUpgrade identifies a result produced by the upgrade command.
const ResultKindUpgrade = "upgrade"

// Mode records how the operation produced its lock.
type Mode string

const (
	ModeReplay  Mode = "replay"
	ModeResolve Mode = "resolve"
	ModeLocked  Mode = "locked"
)

// Change names what happened to one package or to the server record.
type Change string

const (
	ChangeAdded      Change = "added"
	ChangeRemoved    Change = "removed"
	ChangeUpgraded   Change = "upgraded"
	ChangeDowngraded Change = "downgraded"
	ChangeRebuilt    Change = "rebuilt"
	ChangeChanged    Change = "changed"
)

// PackageChange is one difference between the previous lock and the committed
// one, or between no lock and the first one.
type PackageChange struct {
	Reference         string             `json:"reference,omitempty"`
	Name              string             `json:"name"`
	Provider          catalog.Provider   `json:"provider"`
	ProjectID         string             `json:"project_id,omitempty"`
	Version           versionpkg.Version `json:"version,omitempty"`
	Previous          versionpkg.Version `json:"previous,omitempty"`
	ReleaseID         string             `json:"release_id,omitempty"`
	PreviousReleaseID string             `json:"previous_release_id,omitempty"`
	Change            Change             `json:"change"`
	Direct            bool               `json:"direct"`
	RequiredBy        []string           `json:"required_by,omitempty"`
}

// ManifestChange records a declared selector that the operation rewrites.
type ManifestChange struct {
	Reference        string             `json:"reference"`
	PreviousSelector versionpkg.Version `json:"previous_selector"`
	Selector         versionpkg.Version `json:"selector"`
}

// MCDRChange reports a refreshed Python environment for MCDR.
type MCDRChange struct {
	Version        versionpkg.Version `json:"version"`
	Inputs         int                `json:"inputs"`
	PreviousInputs int                `json:"previous_inputs"`
}

// ServerChange reports a new or changed server core.
type ServerChange struct {
	Change    Change             `json:"change,omitempty"`
	Product   server.Product     `json:"product,omitempty"`
	Minecraft versionpkg.Version `json:"minecraft,omitempty"`
	Version   versionpkg.Version `json:"version,omitempty"`
	Previous  versionpkg.Version `json:"previous,omitempty"`
}

// FileChanges counts the managed runtime outputs the commit touched.
type FileChanges struct {
	Created  int `json:"created"`
	Replaced int `json:"replaced"`
	Removed  int `json:"removed"`
}

// FetchSummary counts artifact transfers: local reuse and network downloads.
type FetchSummary struct {
	Cached     int   `json:"cached"`
	Downloaded int   `json:"downloaded"`
	Bytes      int64 `json:"bytes"`
}

// Phase is one timed stage of the operation.
type Phase struct {
	Name    string        `json:"name"`
	Elapsed time.Duration `json:"-"`
	Seconds float64       `json:"seconds"`
}

// Result reports what one operation decided and changed. It carries facts
// only; the command layer renders them.
type Result struct {
	Schema int `json:"schema"`
	// Kind names the machine-readable document. Results created by this
	// package default it to ResultKind; projections from other operations
	// set it explicitly.
	Kind            string                 `json:"kind"`
	Mode            Mode                   `json:"mode"`
	Reason          string                 `json:"reason,omitempty"`
	Policy          string                 `json:"policy,omitempty"`
	PlanHash        string                 `json:"plan_hash,omitempty"`
	Server          ServerChange           `json:"server"`
	Packages        []PackageChange        `json:"packages"`
	ManifestChanges []ManifestChange       `json:"manifest_changes,omitempty"`
	MCDR            *MCDRChange            `json:"mcdr,omitempty"`
	Files           FileChanges            `json:"files"`
	Fetch           FetchSummary           `json:"fetch"`
	Phases          []Phase                `json:"phases"`
	Diagnostics     []filetrack.Diagnostic `json:"diagnostics,omitempty"`
	// Java reports the Java runtime that the server runs on. It is nil when
	// the operation stopped before it selected one.
	Java     *javaruntime.Check `json:"java,omitempty"`
	Warnings []error            `json:"-"`
	// WarningMessages carries Warnings into machine-readable output.
	WarningMessages []string `json:"warnings,omitempty"`
	// TotalPackages counts every package the committed lock records.
	TotalPackages int `json:"total_packages"`
	// PublishedManifest and PublishedLock report which documents changed.
	PublishedManifest bool `json:"published_manifest"`
	PublishedLock     bool `json:"published_lock"`
}

func newResult() *Result {
	return &Result{Schema: ResultSchema, Kind: ResultKind}
}

// CommittedError marks a failure after the installation was committed. The
// published files and documents stay in place; nothing is rolled back.
type CommittedError struct{ Err error }

func (e *CommittedError) Error() string { return e.Err.Error() }

func (e *CommittedError) Unwrap() error { return e.Err }

func (r *Result) addPhase(name string, elapsed time.Duration) {
	if r == nil {
		return
	}
	r.Phases = append(r.Phases, Phase{
		Name:    name,
		Elapsed: elapsed,
		Seconds: elapsed.Seconds(),
	})
}

// Changed reports whether the operation changed the workspace at all.
func (r *Result) Changed() bool {
	if r == nil {
		return false
	}
	return r.Server.Change != "" ||
		len(r.Packages) > 0 ||
		len(r.ManifestChanges) > 0 ||
		r.MCDR != nil ||
		r.PublishedManifest ||
		r.PublishedLock ||
		r.Files.Created > 0 ||
		r.Files.Replaced > 0 ||
		r.Files.Removed > 0
}

func (r *Result) applyChanges(
	before, after *document.Lock,
	d *document.Manifest,
) {
	if r == nil || after == nil {
		return
	}
	r.TotalPackages = len(after.Packages)
	r.Server = serverChange(before, after)
	r.Packages = packageChanges(before, after, d)
}

func serverChange(before, after *document.Lock) ServerChange {
	current := ServerChange{
		Product:   after.Server.Product,
		Minecraft: after.Server.Minecraft,
		Version:   after.Server.CoreVersion,
	}
	switch {
	case before == nil:
		current.Change = ChangeAdded
	case before.Server.Product != after.Server.Product ||
		before.Server.CoreVersion != after.Server.CoreVersion:
		current.Change = ChangeUpgraded
		current.Previous = before.Server.CoreVersion
	}
	return current
}

type packageView struct {
	name      string
	direct    bool
	version   versionpkg.Version
	releaseID string
	fileID    string
	dialect   versionpkg.Dialect
}

func packageChanges(
	before, after *document.Lock,
	d *document.Manifest,
) []PackageChange {
	declared := map[requirementKey]document.Requirement{}
	if d != nil {
		declared = declaredRequirements(d)
	}
	oldPackages := packageViews(before, declared)
	newPackages := packageViews(after, declared)
	dependents := dependencyNames(after, newPackages)
	changes := make([]PackageChange, 0, len(oldPackages)+len(newPackages))
	for id, current := range newPackages {
		previous, existed := oldPackages[id]
		change := PackageChange{
			Name:       current.name,
			Change:     ChangeAdded,
			Direct:     current.direct,
			RequiredBy: dependents[id],
			Version:    current.version,
			ReleaseID:  current.releaseID,
		}
		if pkg := findPackage(after, id); pkg != nil {
			change.Reference = declaredReference(pkg, declared)
			change.Provider = pkg.Provider
			change.ProjectID = pkg.ProjectID
		}
		if existed {
			switch {
			case previous.version == current.version:
				if previous.releaseID == current.releaseID && previous.fileID == current.fileID {
					continue
				}
				change.Change = ChangeRebuilt
				change.Previous = previous.version
				change.PreviousReleaseID = previous.releaseID
			default:
				change.Previous = previous.version
				change.PreviousReleaseID = previous.releaseID
				comparison, comparable := comparePackageVersions(previous, current)
				switch {
				case !comparable:
					change.Change = ChangeChanged
				case comparison < 0:
					change.Change = ChangeUpgraded
				case comparison > 0:
					change.Change = ChangeDowngraded
				default:
					change.Change = ChangeChanged
				}
			}
		}
		changes = append(changes, change)
	}
	for id, previous := range oldPackages {
		if _, still := newPackages[id]; still {
			continue
		}
		change := PackageChange{
			Name:              previous.name,
			Change:            ChangeRemoved,
			Direct:            previous.direct,
			Version:           previous.version,
			Previous:          previous.version,
			PreviousReleaseID: previous.releaseID,
		}
		if pkg := findPackage(before, id); pkg != nil {
			change.Provider = pkg.Provider
			change.ProjectID = pkg.ProjectID
			if reference := enabledReference(pkg); reference != "" {
				change.Reference = reference
				change.Direct = true
			}
		}
		changes = append(changes, change)
	}
	sort.SliceStable(changes, func(i, j int) bool {
		a, b := &changes[i], &changes[j]
		if a.Direct != b.Direct {
			return a.Direct
		}
		return a.Name < b.Name
	})
	return changes
}

func packageViews(
	doc *document.Lock,
	declared map[requirementKey]document.Requirement,
) map[string]packageView {
	views := map[string]packageView{}
	if doc == nil {
		return views
	}
	names := packageNames(doc)
	for i := range doc.Packages {
		p := &doc.Packages[i]
		id := lockedPackageKey(p)
		reference := declaredReference(p, declared)
		if reference == "" {
			reference = enabledReference(p)
		}
		name := names[id]
		if reference != "" {
			name = referenceName(reference)
		}
		direct := reference != ""
		if len(declared) == 0 {
			direct = len(p.Requirements) > 0
		}
		views[id] = packageView{
			name:      name,
			direct:    direct,
			version:   p.Version,
			releaseID: p.ReleaseID,
			fileID:    p.FileID,
			dialect:   packageVersionDialect(p),
		}
	}
	return views
}

// enabledReference returns the reference a package records for itself. A
// package the current manifest no longer names, such as one being removed,
// still carries the requirement that declared it.
func enabledReference(p *document.Package) string {
	for _, binding := range p.Requirements {
		if binding.Enabled {
			return binding.Reference
		}
	}
	if len(p.Requirements) > 0 {
		return p.Requirements[0].Reference
	}
	return ""
}

// packageNames resolves the display name of packages that no requirement
// declares: the module that dependents refer to, else the project identity.
func packageNames(doc *document.Lock) map[string]string {
	names := map[string]string{}
	if doc == nil {
		return names
	}
	for i := range doc.Packages {
		p := &doc.Packages[i]
		id := lockedPackageKey(p)
		if p.ProjectID != "" {
			names[id] = p.ProjectID
		} else {
			names[id] = strings.TrimSuffix(p.Filename, filepath.Ext(p.Filename))
		}
	}
	moduleOwner := moduleOwners(doc)
	for i := range doc.Packages {
		p := &doc.Packages[i]
		for _, module := range p.Modules {
			for _, dependency := range module.Dependencies {
				target := dependencyTarget(p, dependency, moduleOwner, doc)
				if target == "" {
					continue
				}
				used := dependency.ModuleID
				if used == "" {
					used = dependency.ID
				}
				if used != "" {
					names[target] = used
				}
			}
		}
	}
	return names
}

func dependencyNames(
	doc *document.Lock,
	views map[string]packageView,
) map[string][]string {
	dependents := map[string][]string{}
	if doc == nil {
		return dependents
	}
	moduleOwner := moduleOwners(doc)
	for i := range doc.Packages {
		p := &doc.Packages[i]
		source := views[lockedPackageKey(p)].name
		for _, module := range p.Modules {
			for _, dependency := range module.Dependencies {
				target := dependencyTarget(p, dependency, moduleOwner, doc)
				if target == "" {
					continue
				}
				if slices.Contains(dependents[target], source) {
					continue
				}
				dependents[target] = append(dependents[target], source)
			}
		}
	}
	for target := range dependents {
		slices.Sort(dependents[target])
	}
	return dependents
}

func moduleOwners(doc *document.Lock) map[string]string {
	owners := map[string]string{}
	if doc == nil {
		return owners
	}
	for i := range doc.Packages {
		p := &doc.Packages[i]
		id := lockedPackageKey(p)
		for _, module := range p.Modules {
			owners[moduleLookupKey(p, module.ID)] = id
			for _, provided := range module.Provides {
				owners[moduleLookupKey(p, provided)] = id
			}
		}
	}
	return owners
}

func moduleLookupKey(p *document.Package, moduleID string) string {
	return string(p.Environment) + "|" + string(p.Ecosystem) + "|" + strings.ToLower(moduleID)
}

func dependencyTarget(
	source *document.Package,
	dependency catalog.DescriptorDependency,
	moduleOwner map[string]string,
	doc *document.Lock,
) string {
	if dependency.Provider != "" && dependency.ProjectID != "" {
		for i := range doc.Packages {
			candidate := &doc.Packages[i]
			if candidate.Environment == source.Environment &&
				candidate.Ecosystem == source.Ecosystem &&
				candidate.Provider == dependency.Provider &&
				candidate.ProjectID == dependency.ProjectID {
				return lockedPackageKey(candidate)
			}
		}
	}
	moduleID := dependency.ModuleID
	if moduleID == "" {
		moduleID = dependency.ID
	}
	if moduleID == "" {
		return ""
	}
	return moduleOwner[moduleLookupKey(source, moduleID)]
}

func lockedPackageKey(p *document.Package) string {
	identity := p.ProjectID
	if identity == "" {
		identity = p.Filename
	}
	return strings.Join(
		[]string{
			string(p.Environment),
			string(p.Ecosystem),
			string(p.Provider),
			identity,
		},
		"|",
	)
}

func findPackage(doc *document.Lock, id string) *document.Package {
	if doc == nil {
		return nil
	}
	for i := range doc.Packages {
		if lockedPackageKey(&doc.Packages[i]) == id {
			return &doc.Packages[i]
		}
	}
	return nil
}

func declaredReference(
	p *document.Package,
	declared map[requirementKey]document.Requirement,
) string {
	for _, binding := range p.Requirements {
		key := requirementKey{p.Environment, p.Ecosystem, binding.Reference}
		if _, ok := declared[key]; ok {
			return binding.Reference
		}
	}
	return ""
}

func referenceName(reference string) string {
	_, project, err := document.ParseReference(reference)
	if err != nil {
		return reference
	}
	return project
}

func countDeclared(d *document.Manifest) int {
	if d == nil {
		return 0
	}
	count := 0
	for _, packages := range d.Server.Packages {
		for _, requirement := range packages {
			if requirement.IsEnabled() {
				count++
			}
		}
	}
	if d.MCDR != nil {
		for _, requirement := range d.MCDR.Packages {
			if requirement.IsEnabled() {
				count++
			}
		}
	}
	return count
}

func packageVersionDialect(p *document.Package) versionpkg.Dialect {
	for _, module := range p.Modules {
		if module.VersionDialect.Valid() {
			return module.VersionDialect
		}
	}
	switch p.Ecosystem {
	case catalog.EcosystemFabric:
		return versionpkg.DialectFabric
	case catalog.EcosystemForge, catalog.EcosystemNeoForge:
		return versionpkg.DialectMaven
	case catalog.EcosystemMCDR:
		return versionpkg.DialectMCDR
	case catalog.EcosystemBukkit, catalog.EcosystemPaper:
		return versionpkg.DialectOpaque
	default:
		return versionpkg.DialectUnknown
	}
}

func comparePackageVersions(previous, current packageView) (int, bool) {
	dialect := current.dialect
	if !dialect.Valid() {
		dialect = previous.dialect
	}
	if !dialect.Valid() {
		return 0, false
	}
	before, err := versionpkg.ParseVersion(previous.version, dialect)
	if err != nil {
		return 0, false
	}
	after, err := versionpkg.ParseVersion(current.version, dialect)
	if err != nil {
		return 0, false
	}
	return versionpkg.Compare(before, after)
}
