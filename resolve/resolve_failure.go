package resolve

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

// ModuleProblem names the module requirement that a candidate cannot satisfy,
// together with the package that ships the module. The owner lets the failure
// ledger attribute the problem to the package that caused it instead of the
// last package that happened to be selected.
type ModuleProblem struct {
	Environment server.Environment
	Ecosystem   catalog.Ecosystem
	Provider    catalog.Provider
	ProjectID   string
	// RequiredBy names the module that states the requirement, Dependency the
	// module it requires, and Constraint the range that Selected fails.
	RequiredBy string
	Dependency string
	Constraint string
	Selected   string
	// Server marks a requirement the server itself answers. No candidate can
	// change it, so it explains the failure better than any package rejection.
	Server  bool
	Message string
}

func (p ModuleProblem) Error() string { return p.Message }

func moduleProblem(
	owner *document.Package,
	requiredBy string,
	dependency catalog.DescriptorDependency,
	selected string,
	serverProvided bool,
	message string,
) ModuleProblem {
	return ModuleProblem{
		Environment: owner.Environment,
		Ecosystem:   owner.Ecosystem,
		Provider:    owner.Provider,
		ProjectID:   owner.ProjectID,
		RequiredBy:  requiredBy,
		Dependency:  dependency.ID,
		Constraint:  dependency.Constraint,
		Selected:    selected,
		Server:      serverProvided,
		Message:     message,
	}
}

// SearchLimitError reports that resolution stopped before it settled a graph.
// The failure it produces keeps the rejection ledger for the command layer.
type SearchLimitError struct {
	Attempts int
}

func (e *SearchLimitError) Error() string {
	return fmt.Sprintf(
		"package resolution exceeded %d candidate combinations",
		e.Attempts,
	)
}

// RejectionKind names why one candidate was discarded.
type RejectionKind string

const (
	RejectionVersionMismatch RejectionKind = "version-mismatch"
	RejectionReleaseMismatch RejectionKind = "release-mismatch"
	RejectionFilePattern     RejectionKind = "file-pattern"
	RejectionPrimaryFile     RejectionKind = "not-primary-file"
	RejectionChannel         RejectionKind = "not-a-release"
	RejectionModules         RejectionKind = "modules"
	// RejectionServer is a module requirement the server itself answers, such
	// as the loader version. No candidate choice can satisfy it.
	RejectionServer           RejectionKind = "server"
	RejectionMetadataConflict RejectionKind = "metadata-conflict"
	RejectionNested           RejectionKind = "nested"
	RejectionEmpty            RejectionKind = "empty"
)

// RejectionGroup aggregates identical rejections of one project's candidates.
type RejectionGroup struct {
	Kind         RejectionKind
	Detail       string
	Count        int
	FirstVersion string
	LastVersion  string
	// Module facts describe a requirement that rejected the builds. They are
	// empty for kinds that are not about a module.
	RequiredBy string
	Dependency string
	Constraint string
	Selected   string
}

// Failure explains why a project could not be resolved: which project, how
// many builds were considered, and why each group was rejected. The command
// layer renders it; this type only carries facts.
type Failure struct {
	Project    string
	Provider   catalog.Provider
	ProjectID  string
	Minecraft  versionpkg.Version
	Ecosystem  catalog.Ecosystem
	Candidates int
	Groups     []RejectionGroup
	Cause      error
	// Aborted marks a search that stopped before it settled a graph. Project
	// and Groups describe the candidate it rejected most often; Cause names
	// the stop.
	Aborted bool
}

// Error reports the failure on one line. It is the fallback for surfaces that
// do not render the grouped detail.
func (f *Failure) Error() string {
	if f == nil {
		return ""
	}
	if f.Aborted {
		if f.Cause != nil {
			return f.Cause.Error()
		}
		return "package resolution stopped before it settled"
	}
	if len(f.Groups) > 0 && f.Groups[0].Kind == RejectionServer {
		group := f.Groups[0]
		return fmt.Sprintf(
			"%s requires %s %s, but this server runs %s",
			group.RequiredBy,
			group.Dependency,
			group.Constraint,
			group.Selected,
		)
	}
	var parts []string
	for _, group := range f.Groups {
		text := group.Detail
		if group.Count > 1 {
			text = fmt.Sprintf("%s (%d builds)", text, group.Count)
		}
		parts = append(parts, text)
	}
	message := fmt.Sprintf("no build of %s works on this server", f.Project)
	if len(parts) > 0 {
		message += ": " + strings.Join(parts, "; ")
	}
	return message
}

func (f *Failure) Unwrap() error { return f.Cause }

type rejectionKey struct {
	kind   RejectionKind
	detail string
}

type rejectionGroup struct {
	kind         RejectionKind
	detail       string
	count        int
	versions     map[string]struct{}
	firstVersion string
	lastVersion  string
	requiredBy   string
	dependency   string
	constraint   string
	selected     string
}

type projectLedger struct {
	display    string
	provider   catalog.Provider
	projectID  string
	candidates int
	groups     map[rejectionKey]*rejectionGroup
	order      []rejectionKey
}

// rejectionLedger records why candidates were discarded, grouped by project.
// It keeps one entry per distinct reason so the failure summary can report
// "47 builds, all for the same reason" instead of the last error alone.
type rejectionLedger struct {
	projects map[projectKey]*projectLedger
	order    []projectKey
	displays map[projectKey]string
	// failed holds the projects whose resolution gave up. Projects that only
	// rejected builds on the way to a selection are not failures.
	failed map[projectKey]bool
}

func newRejectionLedger() *rejectionLedger {
	return &rejectionLedger{
		projects: map[projectKey]*projectLedger{},
		displays: map[projectKey]string{},
		failed:   map[projectKey]bool{},
	}
}

func (l *rejectionLedger) reset() {
	l.projects = map[projectKey]*projectLedger{}
	l.order = nil
	l.displays = map[projectKey]string{}
	l.failed = map[projectKey]bool{}
}

func (l *rejectionLedger) markFailed(key projectKey) { l.failed[key] = true }

func (l *rejectionLedger) noteDisplay(key projectKey, display string) {
	if display != "" {
		l.displays[key] = display
	}
}

func (l *rejectionLedger) displayFor(key projectKey) string {
	return l.displays[key]
}

func (l *rejectionLedger) project(key projectKey, display string) *projectLedger {
	entry, ok := l.projects[key]
	if !ok {
		entry = &projectLedger{
			display:   display,
			provider:  key.provider,
			projectID: key.project,
			groups:    map[rejectionKey]*rejectionGroup{},
		}
		l.projects[key] = entry
		l.order = append(l.order, key)
	}
	if display != "" {
		entry.display = display
	}
	return entry
}

func (l *rejectionLedger) setCandidates(key projectKey, display string, count int) {
	entry := l.project(key, display)
	if count > entry.candidates {
		entry.candidates = count
	}
}

func (l *rejectionLedger) record(
	key projectKey,
	display string,
	version, detail string,
	kind RejectionKind,
) {
	l.add(l.project(key, display), version, rejectionKey{kind: kind, detail: detail})
}

// recordModule records a module requirement rejection. A requirement the
// server answers becomes a server rejection, which the summary prefers over
// every package-level reason.
func (l *rejectionLedger) recordModule(
	key projectKey,
	display string,
	version string,
	problem ModuleProblem,
) {
	kind := RejectionModules
	if problem.Server {
		kind = RejectionServer
	}
	group := l.add(
		l.project(key, display),
		version,
		rejectionKey{kind: kind, detail: problem.Message},
	)
	group.requiredBy = problem.RequiredBy
	group.dependency = problem.Dependency
	group.constraint = problem.Constraint
	group.selected = problem.Selected
}

func (l *rejectionLedger) add(
	entry *projectLedger,
	version string,
	groupKey rejectionKey,
) *rejectionGroup {
	group, ok := entry.groups[groupKey]
	if !ok {
		group = &rejectionGroup{kind: groupKey.kind, detail: groupKey.detail}
		entry.groups[groupKey] = group
		entry.order = append(entry.order, groupKey)
	}
	group.count++
	if version != "" {
		if group.versions == nil {
			group.versions = map[string]struct{}{}
		}
		group.versions[version] = struct{}{}
		if group.firstVersion == "" {
			group.firstVersion = version
		}
		group.lastVersion = version
	}
	return group
}

// summary picks the project whose rejection explains the failure best: a
// requirement the server cannot satisfy first, then the most specific reasons,
// not the request that happened to be processed first. Nested failures only
// back up a deeper cause.
func (l *rejectionLedger) summary(minecraft versionpkg.Version, cause error) *Failure {
	var bestKey projectKey
	var bestEntry *projectLedger
	bestServer := -1
	bestScore := -1
	bestTotal := -1
	for _, key := range l.order {
		if !l.failed[key] {
			continue
		}
		entry := l.projects[key]
		server := 0
		score := 0
		total := 0
		for _, group := range entry.groups {
			total += group.count
			if group.kind == RejectionServer {
				server += group.count
				continue
			}
			if group.kind != RejectionNested {
				score += group.count
			}
		}
		if total == 0 {
			continue
		}
		better := server > bestServer
		if server == bestServer {
			better = score > bestScore || (score == bestScore && total > bestTotal)
		}
		if better {
			bestKey, bestEntry = key, entry
			bestServer, bestScore, bestTotal = server, score, total
		}
	}
	if bestEntry == nil {
		return nil
	}
	return l.build(bestKey, bestEntry, minecraft, cause)
}

func (l *rejectionLedger) build(
	key projectKey,
	entry *projectLedger,
	minecraft versionpkg.Version,
	cause error,
) *Failure {
	groups := make([]RejectionGroup, 0, len(entry.order))
	for _, groupKey := range entry.order {
		group := entry.groups[groupKey]
		count := group.count
		if len(group.versions) > 0 {
			count = len(group.versions)
		}
		groups = append(groups, RejectionGroup{
			Kind:         group.kind,
			Detail:       group.detail,
			Count:        count,
			FirstVersion: group.firstVersion,
			LastVersion:  group.lastVersion,
			RequiredBy:   group.requiredBy,
			Dependency:   group.dependency,
			Constraint:   group.constraint,
			Selected:     group.selected,
		})
	}
	// A nested failure is the parent's summary of a deeper cause. When the
	// parent also records a reason of its own, the summary adds nothing.
	if len(groups) > 1 {
		groups = slices.DeleteFunc(groups, func(group RejectionGroup) bool {
			return group.Kind == RejectionNested
		})
	}
	// A server constraint explains the failure better than any package
	// rejection, so it leads the groups the renderers and reports read.
	sort.SliceStable(groups, func(i, j int) bool {
		left := groups[i].Kind == RejectionServer
		right := groups[j].Kind == RejectionServer
		if left != right {
			return left
		}
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Detail < groups[j].Detail
	})
	display := entry.display
	if display == "" {
		display = l.displays[key]
	}
	if display == "" {
		display = entry.projectID
	}
	return &Failure{
		Project:    display,
		Provider:   entry.provider,
		ProjectID:  entry.projectID,
		Minecraft:  minecraft,
		Ecosystem:  key.ecosystem,
		Candidates: entry.candidates,
		Groups:     groups,
		Cause:      cause,
	}
}

func problemErrors(problems []ModuleProblem) []error {
	errs := make([]error, 0, len(problems))
	for _, problem := range problems {
		errs = append(errs, problem)
	}
	return errs
}

// resolutionFailure prefers the structured explanation over the last error,
// which only names the request that happened to fail last.
func resolutionFailure(failure *Failure, cause error) error {
	if failure != nil {
		return failure
	}
	return cause
}

func describeRejection(kind RejectionKind, detail string) string {
	switch kind {
	case RejectionVersionMismatch:
		return "does not match " + detail
	case RejectionReleaseMismatch:
		return "release " + detail + " expected"
	case RejectionFilePattern:
		return "filename does not match " + detail
	case RejectionPrimaryFile:
		return "not the primary file"
	case RejectionChannel:
		return "not a release build"
	case RejectionMetadataConflict:
		return "conflicts with " + detail
	default:
		return detail
	}
}
