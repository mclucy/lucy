package resolve

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"

	versionpkg "github.com/mclucy/lucy/version"
)

// moduleScope evaluates the dependency tree of one module against the modules
// a graph selects. Identities are scoped by the environment of the declaring
// package and by the target's module namespace for the module's ecosystem.
type moduleScope struct {
	target      Target
	environment serverpkg.Environment
	ecosystem   catalog.Ecosystem
	dialect     versionpkg.Dialect
	runtime     map[string]catalog.DescriptorModule
	// index is nil when only the modules of the server runtime count.
	index      map[moduleKey]moduleProvider
	clientOnly map[moduleKey]struct{}
}

// requirementOutcome is the state of the selected graph that one dependency
// node observes.
type requirementOutcome struct {
	// present reports that a module answers an identity the node names.
	present bool
	// matched reports that the node holds.
	matched bool
	// waived reports a node that holds without a module to bind: an unless
	// condition holds, an any-of group has an absent optional alternative, or
	// the only candidate is client-only.
	waived    bool
	value     string
	owner     *moduleProvider
	matchedID string
}

func (s moduleScope) key(id string) moduleKey {
	return moduleKey{
		s.environment,
		s.target.moduleNamespace(s.ecosystem),
		normalizeModuleID(s.ecosystem, id),
	}
}

// lookup returns the selected version of an identity and the package module
// that answers it. A runtime module answers before any package and has no
// owner.
func (s moduleScope) lookup(id string) (string, *moduleProvider) {
	if module, ok := s.runtime[normalizeModuleID(s.ecosystem, id)]; ok {
		return string(module.Version), nil
	}
	if provider, ok := s.index[s.key(id)]; ok {
		return string(provider.module.Version), &provider
	}
	return "", nil
}

// evaluate decides one dependency node. A node holds when its own identity or
// one of its AnyOf alternatives holds, and every AllOf node holds too. A
// holding Unless condition waives the node; a waived break does not apply.
// grouped marks a node that is an alternative of an any-of group.
func (s moduleScope) evaluate(
	dep *catalog.DescriptorDependency,
	grouped bool,
) (requirementOutcome, error) {
	exempt, err := s.exempted(dep)
	if err != nil {
		return requirementOutcome{}, err
	}
	if exempt {
		holds := dep.Kind != catalog.DependencyKindIncompatible
		return requirementOutcome{matched: holds, waived: holds}, nil
	}
	outcome, err := s.leaf(dep, grouped || len(dep.AnyOf) > 0)
	if err != nil {
		return outcome, err
	}
	for i := 0; !outcome.matched && i < len(dep.AnyOf); i++ {
		alternative, err := s.evaluate(&dep.AnyOf[i], true)
		if err != nil {
			return outcome, err
		}
		if alternative.matched {
			alternative.present = alternative.present || outcome.present
			outcome = alternative
		} else if !outcome.present && alternative.present {
			outcome.present = true
			outcome.value = alternative.value
		}
	}
	if !outcome.matched || outcome.waived {
		return outcome, nil
	}
	for i := range dep.AllOf {
		condition, err := s.evaluate(&dep.AllOf[i], false)
		if err != nil {
			return outcome, err
		}
		if !condition.matched {
			outcome.matched = false
			return outcome, nil
		}
	}
	return outcome, nil
}

// leaf decides the identity a node names, without its alternatives.
func (s moduleScope) leaf(
	dep *catalog.DescriptorDependency,
	grouped bool,
) (requirementOutcome, error) {
	value, owner := s.lookup(dep.ID)
	outcome := requirementOutcome{
		present: value != "",
		value:   value,
		owner:   owner,
	}
	var err error
	switch {
	case value != "":
		outcome.matched, err = matchModuleConstraint(dep, value, s.target, s.dialect)
	case dep.Constraint != "":
		// A loader support matrix can answer a module no package selects.
		outcome.matched, err = matchesLoaderCompatibility(
			dep.Constraint,
			dep.ID,
			s.target,
			s.dialect,
		)
	}
	if err != nil {
		return outcome, err
	}
	if outcome.matched {
		outcome.matchedID = dep.ID
		return outcome, nil
	}
	if value != "" || dep.Kind == catalog.DependencyKindIncompatible {
		return outcome, nil
	}
	if _, clientOnly := s.clientOnly[s.key(dep.ID)]; clientOnly ||
		(grouped && dep.Kind == catalog.DependencyKindOptional) {
		// The loader filters a client-only candidate out of a dedicated
		// server, and an absent optional alternative satisfies its group.
		return requirementOutcome{matched: true, waived: true}, nil
	}
	return outcome, nil
}

// exempted reports whether an Unless condition of dep holds. A condition
// holds only when a selected module answers it.
func (s moduleScope) exempted(dep *catalog.DescriptorDependency) (bool, error) {
	for i := range dep.Unless {
		condition, err := s.evaluate(&dep.Unless[i], false)
		if err != nil {
			return false, err
		}
		if condition.present && condition.matched {
			return true, nil
		}
	}
	return false, nil
}

// matchModuleConstraint applies one dependency constraint to a selected
// version, including the loader support matrices that substitute versions for
// modules the loader knows about.
func matchModuleConstraint(
	dep *catalog.DescriptorDependency,
	value string,
	target Target,
	dialect versionpkg.Dialect,
) (bool, error) {
	if fmlLoader(target) && value == "0.0NONE" {
		// FMLLoader treats the 0.0NONE sentinel as satisfying every range.
		return true, nil
	}
	if dep.Constraint == "" {
		return true, nil
	}
	matched, err := versionpkg.Match(
		dep.Constraint,
		versionpkg.Version(value),
		dialect,
	)
	if err != nil || matched {
		return matched, err
	}
	return matchesLoaderCompatibility(dep.Constraint, dep.ID, target, dialect)
}

// checkModuleDependencies reports the problems of one module's dependencies
// and binds each holding dependency to the package module that answers it.
func checkModuleDependencies(
	p *document.Package,
	m catalog.DescriptorModule,
	scope moduleScope,
) []ModuleProblem {
	var problems []ModuleProblem
	for di := range m.Dependencies {
		dep := &m.Dependencies[di]
		// A Java requirement is checked against the Java runtime that runs
		// the server, and only warns.
		if dep.Kind == catalog.DependencyKindPython || dep.ID == "java" {
			continue
		}
		outcome, err := scope.evaluate(dep, false)
		if err != nil {
			problems = append(problems, moduleProblem(
				p,
				m.ID,
				*dep,
				outcome.value,
				false,
				fmt.Sprintf("module %s requires %s: %v", m.ID, dep.ID, err),
			))
			continue
		}
		if message := dependencyProblemMessage(m.ID, dep, outcome); message != "" {
			problems = append(problems, moduleProblem(
				p,
				m.ID,
				*dep,
				outcome.value,
				outcome.owner == nil && outcome.value != "",
				message,
			))
		}
		if !outcome.matched || outcome.waived {
			continue
		}
		switch {
		case outcome.owner != nil && !outcome.owner.source.runtime():
			dep.Provider = outcome.owner.provider
			dep.ProjectID = outcome.owner.projectID
			dep.ModuleID = outcome.owner.module.ID
		case outcome.matchedID != "":
			dep.ModuleID = outcome.matchedID
		default:
			dep.ModuleID = dep.ID
		}
	}
	return problems
}

// dependencyProblemMessage describes the problem an evaluated dependency
// causes, or returns "" when it causes none. An optional dependency outside
// an any-of group is checked only when a module answers it.
func dependencyProblemMessage(
	requiredBy string,
	dep *catalog.DescriptorDependency,
	outcome requirementOutcome,
) string {
	requirement := dependencyRequirementText(dep)
	switch {
	case dep.Kind == catalog.DependencyKindIncompatible:
		if outcome.matched {
			breaks := dep.ID
			if outcome.matchedID != "" {
				breaks = outcome.matchedID
			}
			return fmt.Sprintf(
				"module %s breaks %s %s",
				requiredBy,
				breaks,
				outcome.value,
			)
		}
	case dep.Kind == catalog.DependencyKindRequired ||
		(dep.Kind == catalog.DependencyKindOptional && len(dep.AnyOf) > 0):
		if outcome.matched {
			return ""
		}
		if outcome.value == "" {
			return fmt.Sprintf(
				"module %s requires %s, which no selected package provides",
				requiredBy,
				requirement,
			)
		}
		return fmt.Sprintf(
			"module %s requires %s (selected %s)",
			requiredBy,
			requirement,
			outcome.value,
		)
	case dep.Kind == catalog.DependencyKindOptional:
		if outcome.present && !outcome.matched {
			return fmt.Sprintf(
				"module %s optionally requires %s (selected %s)",
				requiredBy,
				requirement,
				outcome.value,
			)
		}
	}
	return ""
}

func dependencyRequirementText(dep *catalog.DescriptorDependency) string {
	parts := make([]string, 0, 1+len(dep.AnyOf))
	parts = append(parts, strings.TrimSpace(dep.ID+" "+dep.Constraint))
	for i := range dep.AnyOf {
		alternative := &dep.AnyOf[i]
		parts = append(
			parts,
			strings.TrimSpace(alternative.ID+" "+alternative.Constraint),
		)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "one of " + strings.Join(parts, ", ")
}
