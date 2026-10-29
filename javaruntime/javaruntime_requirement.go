// Package javaruntime decides which Java runtime a server runs on and how
// that runtime misses the requirement of the locked server. Every miss is a
// warning: Lucy never refuses to run a server because of its Java runtime.
package javaruntime

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/version"
)

// dependencyID is the dependency name that package descriptors use for the
// Java runtime.
const dependencyID = "java"

// newestJava bounds the search for a major version that meets every
// constraint.
const newestJava = 99

type constraintKind int

const (
	constraintAdvisory constraintKind = iota
	constraintPin
	constraintPackage
	constraintIncompatible
)

// Constraint is one condition on the Java major version.
type Constraint struct {
	// Source names who sets the condition, for example "Minecraft 1.20.1" or
	// "carpet 1.4.112".
	Source string `json:"source"`
	// Expression is the condition as its source writes it. For a package that
	// declares an incompatibility, it is the range the package rejects.
	Expression string `json:"expression"`
	kind       constraintKind
	accepts    func(major int) bool
}

// Accepts reports whether Java major meets c.
func (c Constraint) Accepts(major int) bool {
	return c.accepts(major)
}

// miss describes how Java major fails c.
func (c Constraint) miss(major int) string {
	switch c.kind {
	case constraintAdvisory:
		return fmt.Sprintf(
			"Java %d is older than the Java version that Mojang recommends for %s (%s)",
			major,
			c.Source,
			c.Expression,
		)
	case constraintPin:
		return fmt.Sprintf("Java %d is not Java %s, which %s pins", major, c.Expression, c.Source)
	case constraintIncompatible:
		return fmt.Sprintf("%s is incompatible with Java %s", c.Source, c.Expression)
	default:
		return fmt.Sprintf("%s requires Java %s", c.Source, c.Expression)
	}
}

// Requirement is the Java runtime that a locked server asks for.
type Requirement struct {
	// Advisory is the major version that Mojang recommends.
	Advisory int `json:"advisory"`
	// Version is the major version that the manifest pins; zero follows the
	// constraints.
	Version int `json:"version,omitempty"`
	// Distribution is the distribution that the manifest pins; empty accepts
	// every build.
	Distribution java.Distribution `json:"distribution,omitempty"`
	Constraints  []Constraint      `json:"constraints"`
}

// RequirementOf combines the Java inputs that lock records: the Mojang
// advisory, the manifest pin, and the Java requirements of the enabled
// packages.
func RequirementOf(lock *document.Lock, logger *log.Logger) Requirement {
	settings := lock.Server.Java
	r := Requirement{
		Advisory:     settings.Advisory,
		Version:      settings.Version,
		Distribution: settings.Distribution,
	}
	advisory := settings.Advisory
	r.Constraints = append(r.Constraints, Constraint{
		Source:     "Minecraft " + lock.Server.Minecraft.String(),
		Expression: fmt.Sprintf(">=%d", advisory),
		kind:       constraintAdvisory,
		accepts:    func(major int) bool { return major >= advisory },
	})
	if pinned := settings.Version; pinned != 0 {
		r.Constraints = append(r.Constraints, Constraint{
			Source:     document.ManifestFilename,
			Expression: strconv.Itoa(pinned),
			kind:       constraintPin,
			accepts:    func(major int) bool { return major == pinned },
		})
	}
	for i := range lock.Packages {
		p := &lock.Packages[i]
		if !p.Enabled() {
			continue
		}
		for _, m := range p.Modules {
			for _, dep := range m.Dependencies {
				if c, ok := packageConstraint(m, dep, advisory, logger); ok &&
					!slices.ContainsFunc(r.Constraints, func(known Constraint) bool {
						return known.Source == c.Source && known.Expression == c.Expression && known.kind == c.kind
					}) {
					r.Constraints = append(r.Constraints, c)
				}
			}
		}
	}
	return r
}

func packageConstraint(
	m catalog.DescriptorModule,
	dep catalog.DescriptorDependency,
	advisory int,
	logger *log.Logger,
) (Constraint, bool) {
	if dep.ID != dependencyID || dep.Constraint == "" {
		return Constraint{}, false
	}
	kind := constraintPackage
	switch dep.Kind {
	case catalog.DependencyKindRequired:
	case catalog.DependencyKindIncompatible:
		kind = constraintIncompatible
	default:
		return Constraint{}, false
	}
	expression, dialect := dep.Constraint, m.VersionDialect
	match := func(major int) (bool, error) {
		return version.Match(expression, version.Version(strconv.Itoa(major)), dialect)
	}
	if _, err := match(advisory); err != nil {
		logger.Debug(
			"java requirement ignored",
			"module", m.ID,
			"constraint", expression,
			"reason", err,
		)
		return Constraint{}, false
	}
	return Constraint{
		Source:     m.ID + " " + m.Version.String(),
		Expression: expression,
		kind:       kind,
		accepts: func(major int) bool {
			matched, _ := match(major)
			return matched != (kind == constraintIncompatible)
		},
	}, true
}

// Unmet returns the constraints that Java major fails.
func (r Requirement) Unmet(major int) []Constraint {
	var unmet []Constraint
	for _, c := range r.Constraints {
		if !c.Accepts(major) {
			unmet = append(unmet, c)
		}
	}
	return unmet
}

// Lowest returns the oldest major version that meets every constraint, and
// false when no version does.
func (r Requirement) Lowest() (int, bool) {
	for major := document.MinimumJava; major <= newestJava; major++ {
		if len(r.Unmet(major)) == 0 {
			return major, true
		}
	}
	return 0, false
}

// Target returns the major version to install: the pin, else the oldest
// version that meets every constraint, else the advisory.
func (r Requirement) Target() int {
	if r.Version != 0 {
		return r.Version
	}
	if lowest, ok := r.Lowest(); ok {
		return lowest
	}
	return r.Advisory
}
