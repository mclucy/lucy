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
	FindingUnmanagedInstallation       FindingKind = "unmanaged_installation"
	FindingMissingDeclaredInstallation FindingKind = "missing_declared_installation"
	FindingUnmatchedInstallations      FindingKind = "unmatched_installations"
	FindingAmbiguousInstallations      FindingKind = "ambiguous_installations"
	FindingAutomaticSelection          FindingKind = "automatic_selection"
)

// Finding is one typed observation condition. Path is the primary affected
// path; Paths carries related evidence when a condition spans several files.
// Installation conditions carry Identity or Identities instead of entry-point
// paths.
type Finding struct {
	Kind       FindingKind
	Path       string
	Paths      []string
	Identity   Identity
	Identities []Identity
	Ownership  Ownership
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

// Selected returns the current selection only when it names a launchable
// installation.
func (r *Result) Selected() *Selection {
	if r == nil || !r.Selection.IsValid() {
		return nil
	}
	return &r.Selection
}

// FindInstallation returns the installation whose member paths and roles
// exactly match the given evidence. It returns nil when no installation
// matches.
func (r *Result) FindInstallation(members map[string]FileRole) *Installation {
	if r == nil || len(members) == 0 {
		return nil
	}
	for index := range r.Installations {
		installation := &r.Installations[index]
		if installationMatchesMembers(installation, members) {
			return installation
		}
	}
	return nil
}

func installationMatchesMembers(
	installation *Installation,
	members map[string]FileRole,
) bool {
	if installation == nil || len(installation.Files) != len(members) {
		return false
	}
	for _, file := range installation.Files {
		role, ok := members[file.Path]
		if !ok || role != file.Role {
			return false
		}
	}
	return true
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
			Identities: r.candidateIdentities(),
		})
	case SelectionUnmatched:
		return yield(Finding{
			Kind:       FindingUnmatchedInstallations,
			Identities: r.observedIdentities(),
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
			Identity:   selected.Installation.Identity,
			Identities: r.competingIdentities(),
		}) {
			return false
		}
	}
	for index := range r.Installations {
		installation := &r.Installations[index]
		if installation == selected.Installation ||
			!launchableInstallation(installation) {
			continue
		}
		entry := chooseEntryPoint(installation)
		if entry == nil || entry.Ownership == OwnershipManaged {
			continue
		}
		if !yield(Finding{
			Kind:      FindingUnmanagedInstallation,
			Identity:  installation.Identity,
			Ownership: entry.Ownership,
		}) {
			return false
		}
	}
	return true
}

// observedIdentities lists every valid identity a filter could have matched.
func (r *Result) observedIdentities() []Identity {
	identities := make([]Identity, 0, len(r.Installations))
	for index := range r.Installations {
		installation := &r.Installations[index]
		if !installation.Identity.IsValid() {
			continue
		}
		identities = append(identities, installation.Identity)
	}
	return uniqueIdentities(identities)
}

func (r *Result) candidateIdentities() []Identity {
	identities := make([]Identity, 0, len(r.Installations))
	for index := range r.Installations {
		installation := &r.Installations[index]
		if !launchableInstallation(installation) {
			continue
		}
		identities = append(identities, installation.Identity)
	}
	return uniqueIdentities(identities)
}

func (r *Result) competingIdentities() []Identity {
	if r == nil || r.Selection.Installation == nil {
		return nil
	}
	identities := make([]Identity, 0, len(r.Installations))
	for index := range r.Installations {
		installation := &r.Installations[index]
		if installation == r.Selection.Installation ||
			!launchableInstallation(installation) {
			continue
		}
		identities = append(identities, installation.Identity)
	}
	return uniqueIdentities(identities)
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
