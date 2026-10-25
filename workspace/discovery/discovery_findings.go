package discovery

import (
	"iter"
	"slices"
	"sort"
)

// FindingKind is a typed observation condition. Presentation severity and CLI
// suggestions belong to the presentation boundary.
type FindingKind string

const (
	FindingUnknown FindingKind = ""

	FindingUnknownArtifact             FindingKind = "unknown_artifact"
	FindingConflictingIdentities       FindingKind = "conflicting_identities"
	FindingInvalidIdentity             FindingKind = "invalid_identity"
	FindingUnreadableFile              FindingKind = "unreadable_file"
	FindingReceiptError                FindingKind = "receipt_error"
	FindingOwnershipError              FindingKind = "ownership_error"
	FindingModifiedFile                FindingKind = "modified_file"
	FindingAlternativeEntryPoint       FindingKind = "alternative_entry_point"
	FindingUnmanagedInstallation       FindingKind = "unmanaged_installation"
	FindingMissingDeclaredInstallation FindingKind = "missing_declared_installation"
	FindingAmbiguousInstallations      FindingKind = "ambiguous_installations"
	FindingAutomaticSelection          FindingKind = "automatic_selection"
)

// Finding is one typed observation condition. Path is the primary affected
// path; Paths carries every competing or related path when the condition spans
// several entry points.
type Finding struct {
	Kind       FindingKind
	Path       string
	Paths      []string
	Identity   Identity
	Identities []Identity
	Cause      error
}

// Result is one discovery observation. Installations are immutable after
// Discover except for ownership labels applied by ObserveOwnership. Selection
// is the explicit read-only choice made by Select.
type Result struct {
	Installations []Installation
	Diagnostics   []Finding
	Selection     Selection
}

// Selected returns the current selection only when it names a selected
// installation and entry point.
func (r *Result) Selected() *Selection {
	if r == nil || !r.Selection.IsValid() {
		return nil
	}
	return &r.Selection
}

// Findings yields the immutable diagnostics followed by selection conditions
// derived from the current selection. Nothing is cached between calls, so a
// changed selection never leaves stale finding arrays behind.
func (r *Result) Findings() iter.Seq[Finding] {
	return func(yield func(Finding) bool) {
		if r == nil {
			return
		}
		for _, finding := range r.Diagnostics {
			if !yield(finding) {
				return
			}
		}
		r.yieldSelectionFindings(yield)
	}
}

func (r *Result) yieldSelectionFindings(yield func(Finding) bool) bool {
	switch r.Selection.State {
	case SelectionSelected:
		return r.yieldSelectedFindings(yield)
	case SelectionAmbiguous:
		return yield(Finding{
			Kind:       FindingAmbiguousInstallations,
			Path:       r.Selection.RequestedPath,
			Paths:      r.candidateEntryPoints(),
			Identities: r.candidateIdentities(),
		})
	case SelectionMissingDeclared:
		return yield(Finding{
			Kind: FindingMissingDeclaredInstallation,
		})
	default:
		return true
	}
}

func (r *Result) yieldSelectedFindings(yield func(Finding) bool) bool {
	selected := r.Selection
	if !selected.IsValid() {
		return true
	}
	if selected.Source == SourcePreferred {
		if !yield(Finding{
			Kind:       FindingAutomaticSelection,
			Path:       selected.EntryPoint.Path,
			Paths:      r.competingEntryPoints(),
			Identity:   selected.Installation.Identity,
			Identities: r.competingIdentities(),
		}) {
			return false
		}
	}
	for file := range selected.Installation.EntryPoints() {
		if file.Path == selected.EntryPoint.Path ||
			file.Ownership == OwnershipManaged {
			continue
		}
		if !yield(Finding{
			Kind:     FindingAlternativeEntryPoint,
			Path:     file.Path,
			Identity: selected.Installation.Identity,
		}) {
			return false
		}
	}
	for index := range r.Installations {
		installation := &r.Installations[index]
		if installation == selected.Installation ||
			!selectableInstallation(installation) {
			continue
		}
		for file := range installation.EntryPoints() {
			if file.Ownership == OwnershipManaged {
				continue
			}
			if !yield(Finding{
				Kind:     FindingUnmanagedInstallation,
				Path:     file.Path,
				Identity: installation.Identity,
			}) {
				return false
			}
		}
	}
	return true
}

func (r *Result) candidateEntryPoints() []string {
	paths := make([]string, 0, len(r.Installations))
	for index := range r.Installations {
		installation := &r.Installations[index]
		if !selectableInstallation(installation) {
			continue
		}
		for file := range installation.EntryPoints() {
			paths = append(paths, file.Path)
		}
	}
	return sortedUnique(paths)
}

func (r *Result) candidateIdentities() []Identity {
	identities := make([]Identity, 0, len(r.Installations))
	for index := range r.Installations {
		installation := &r.Installations[index]
		if !selectableInstallation(installation) {
			continue
		}
		identities = append(identities, installation.Identity)
	}
	return uniqueIdentities(identities)
}

func (r *Result) competingEntryPoints() []string {
	if r == nil || r.Selection.Installation == nil {
		return nil
	}
	selectedPath := ""
	if r.Selection.EntryPoint != nil {
		selectedPath = r.Selection.EntryPoint.Path
	}
	paths := make([]string, 0, len(r.Installations))
	for index := range r.Installations {
		installation := &r.Installations[index]
		if installation == r.Selection.Installation ||
			!selectableInstallation(installation) {
			continue
		}
		for file := range installation.EntryPoints() {
			if file.Path == selectedPath {
				continue
			}
			paths = append(paths, file.Path)
		}
	}
	return sortedUnique(paths)
}

func (r *Result) competingIdentities() []Identity {
	if r == nil || r.Selection.Installation == nil {
		return nil
	}
	identities := make([]Identity, 0, len(r.Installations))
	for index := range r.Installations {
		installation := &r.Installations[index]
		if installation == r.Selection.Installation ||
			!selectableInstallation(installation) {
			continue
		}
		identities = append(identities, installation.Identity)
	}
	return uniqueIdentities(identities)
}

func selectableInstallation(installation *Installation) bool {
	return installation != nil &&
		installation.Identity.IsValid() &&
		firstEntryPoint(installation) != nil
}

// uniqueIdentities sorts the caller-owned slice and compacts equal canonical
// identities in place.
func uniqueIdentities(identities []Identity) []Identity {
	sort.SliceStable(identities, func(i, j int) bool {
		return compareIdentities(identities[i], identities[j]) < 0
	})
	unique := identities[:0]
	last := ""
	for _, identity := range identities {
		key := identityKey(identity)
		if len(unique) > 0 && key == last {
			continue
		}
		last = key
		unique = append(unique, identity)
	}
	clear(identities[len(unique):])
	return unique
}

func sortedUnique(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	slices.Sort(paths)
	unique := paths[:0]
	for _, path := range paths {
		if len(unique) > 0 && unique[len(unique)-1] == path {
			continue
		}
		unique = append(unique, path)
	}
	return unique
}

func nonEmptyPaths(path string) []string {
	if path == "" {
		return nil
	}
	return []string{path}
}
