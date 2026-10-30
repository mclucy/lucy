package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mclucy/lucy/resolve"
	versionpkg "github.com/mclucy/lucy/version"
)

const (
	maxFactChainTypes = 12
	maxFactFailures   = 5
	maxFactRejections = 8
	maxFactTextBytes  = 256
	maxFactDepth      = 32
)

// genericErrorTypes name wrapping errors that carry no fact of their own.
// They stay out of the chain so a failure document does not grow a key for
// every layer the command boundary happens to add.
var genericErrorTypes = map[string]bool{
	"report.Detail":      true,
	"errors.errorString": true,
	"errors.joinError":   true,
	"fmt.wrapError":      true,
}

// FailureFacts returns the machine-readable evidence behind a failure: the
// typed errors on the way to the root cause, and the facts they carry. It
// walks the whole error tree, including joined children, because errors.As
// only reaches the first match.
//
// The result holds identifiers, enum names, and counts. It never holds free
// text other than the rejection details the resolver already formats from
// module identifiers, and it returns nil when the tree holds no typed error,
// so ordinary failures keep an unchanged error document.
func FailureFacts(err error) map[string]any {
	if err == nil {
		return nil
	}
	collector := factCollector{seenTypes: map[string]bool{}}
	collector.walk(err, 0)
	return collector.facts()
}

type factCollector struct {
	types     []string
	seenTypes map[string]bool
	failures  []failureFacts
	attempts  int
}

type failureFacts struct {
	key         string
	project     string
	provider    string
	projectID   string
	minecraft   string
	ecosystem   string
	candidates  int
	aborted     bool
	occurrences int
	rejections  []rejectionFacts
}

type rejectionFacts struct {
	kind         string
	detail       string
	count        int
	firstVersion string
	lastVersion  string
	requiredBy   string
	dependency   string
	constraint   string
	selected     string
}

func (c *factCollector) walk(err error, depth int) {
	if err == nil || depth > maxFactDepth {
		return
	}
	c.addType(fmt.Sprintf("%T", err))
	switch node := err.(type) {
	case *resolve.Failure:
		c.addFailure(node)
	case *resolve.SearchLimitError:
		if node.Attempts > c.attempts {
			c.attempts = node.Attempts
		}
	}
	switch node := err.(type) {
	case interface{ Unwrap() []error }:
		for _, child := range node.Unwrap() {
			c.walk(child, depth+1)
		}
	case interface{ Unwrap() error }:
		c.walk(node.Unwrap(), depth+1)
	}
}

func (c *factCollector) addType(name string) {
	name = strings.TrimPrefix(name, "*")
	if name == "" || genericErrorTypes[name] || c.seenTypes[name] || len(c.types) >= maxFactChainTypes {
		return
	}
	c.seenTypes[name] = true
	c.types = append(c.types, name)
}

func (c *factCollector) addFailure(failure *resolve.Failure) {
	if failure == nil {
		return
	}
	key := failureKey(failure)
	for index := range c.failures {
		if c.failures[index].key == key {
			c.failures[index].occurrences++
			return
		}
	}
	if len(c.failures) >= maxFactFailures {
		return
	}
	entry := failureFacts{
		key:         key,
		project:     factText(failure.Project),
		provider:    string(failure.Provider),
		projectID:   factText(failure.ProjectID),
		minecraft:   factVersion(failure.Minecraft),
		ecosystem:   string(failure.Ecosystem),
		candidates:  failure.Candidates,
		aborted:     failure.Aborted,
		occurrences: 1,
	}
	for _, group := range failure.Groups {
		if len(entry.rejections) >= maxFactRejections {
			break
		}
		entry.rejections = append(entry.rejections, rejectionFacts{
			kind:         string(group.Kind),
			detail:       factText(group.Detail),
			count:        group.Count,
			firstVersion: factText(group.FirstVersion),
			lastVersion:  factText(group.LastVersion),
			requiredBy:   factText(group.RequiredBy),
			dependency:   factText(group.Dependency),
			constraint:   factText(group.Constraint),
			selected:     factText(group.Selected),
		})
	}
	c.failures = append(c.failures, entry)
}

func failureKey(failure *resolve.Failure) string {
	parts := []string{
		failure.Project,
		failure.ProjectID,
		fmt.Sprint(failure.Aborted),
	}
	for _, group := range failure.Groups {
		parts = append(parts, string(group.Kind), group.Detail)
	}
	return strings.Join(parts, "\x00")
}

func (c *factCollector) facts() map[string]any {
	facts := map[string]any{}
	if len(c.types) > 0 {
		facts["chain"] = c.types
	}
	if c.attempts > 0 {
		facts["search_limit"] = map[string]any{"attempts": c.attempts}
	}
	if len(c.failures) > 0 {
		entries := make([]map[string]any, 0, len(c.failures))
		for _, failure := range c.failures {
			entries = append(entries, failure.entry())
		}
		facts["failures"] = entries
	}
	if len(facts) == 0 {
		return nil
	}
	return facts
}

func (f failureFacts) entry() map[string]any {
	entry := map[string]any{}
	if f.project != "" {
		entry["project"] = f.project
	}
	if f.projectID != "" {
		entry["project_id"] = f.projectID
	}
	if f.provider != "" {
		entry["provider"] = f.provider
	}
	if f.ecosystem != "" {
		entry["ecosystem"] = f.ecosystem
	}
	if f.minecraft != "" {
		entry["minecraft"] = f.minecraft
	}
	if f.candidates > 0 {
		entry["candidates"] = f.candidates
	}
	if f.aborted {
		entry["aborted"] = true
	}
	if f.occurrences > 1 {
		entry["occurrences"] = f.occurrences
	}
	if len(f.rejections) > 0 {
		rejections := make([]map[string]any, 0, len(f.rejections))
		for _, rejection := range f.rejections {
			rejections = append(rejections, rejection.entry())
		}
		entry["rejections"] = rejections
	}
	return entry
}

func (r rejectionFacts) entry() map[string]any {
	entry := map[string]any{"kind": r.kind}
	if r.detail != "" {
		entry["detail"] = r.detail
	}
	if r.count > 0 {
		entry["count"] = r.count
	}
	if r.firstVersion != "" {
		entry["first_version"] = r.firstVersion
	}
	if r.lastVersion != "" {
		entry["last_version"] = r.lastVersion
	}
	if r.requiredBy != "" {
		entry["required_by"] = r.requiredBy
	}
	if r.dependency != "" {
		entry["dependency"] = r.dependency
	}
	if r.constraint != "" {
		entry["constraint"] = r.constraint
	}
	if r.selected != "" {
		entry["selected"] = r.selected
	}
	return entry
}

// factVersion renders a version fact, and drops the values that describe no
// target: the zero version and the wildcard both mean the failure was not
// bound to one Minecraft version.
func factVersion(value versionpkg.Version) string {
	if value == "" || value == versionpkg.Any {
		return ""
	}
	return factText(value.String())
}

// factText bounds one fact string. Identifiers and versions are far shorter
// than the bound; it only exists so a rejection detail cannot grow the report.
func factText(value string) string {
	return truncateFactText(value, maxFactTextBytes)
}

func truncateFactText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut] + "…"
}
