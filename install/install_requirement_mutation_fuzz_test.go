package install_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

const (
	mutationAbsent = iota
	mutationDisabled
	mutationEnabled
)

var mutationNames = [3]string{"alpha", "beta", "gamma"}

type mutationScenario struct {
	nodes  int
	edges  [][2]int
	first  []int
	second []int
}

// FuzzPlanInstallLockedRequirementMutation exercises locked requirement
// mutations over a generated dependency graph. The oracle derives retained and
// active packages by enumerating simple paths from declared and enabled
// packages respectively, and derives the dependency provenance the lock must
// persist from the retained edges. It asserts the replay behavior consumers
// observe: retained identities, Enabled, dependency provenance, direct binding
// intent, and unchanged pins.
//
// Round one mutates every initial direct root. Round two mutates only packages
// retained by round one, including adding a disabled binding to a dormant,
// source-backed package. Round three re-enables every retained declaration.
// Input layout: the first byte selects the node count as 1+byte%3, then one
// byte per ordered source/target pair (odd means the edge exists), then one
// mutation byte per node for round one, then one per node for round two. A
// state is 0 absent, 1 disabled, 2 enabled.
func FuzzPlanInstallLockedRequirementMutation(f *testing.F) {
	// alpha requires beta; removing both roots leaves no reachability seed, so
	// beta does not survive on historical dependency state alone.
	f.Add([]byte{1, 1, 0, mutationAbsent, mutationAbsent, mutationAbsent, mutationAbsent})
	// alpha keeps a disabled declaration, beta loses its own. Beta stays pinned
	// as a dormant dependency, then receives a newly added disabled binding.
	f.Add([]byte{
		1, 1, 0,
		mutationDisabled, mutationAbsent,
		mutationDisabled, mutationDisabled,
	})
	// A removed root remains active while an enabled ancestor reaches it.
	f.Add([]byte{
		1, 1, 0,
		mutationEnabled, mutationAbsent,
		mutationEnabled, mutationAbsent,
	})
	// A dependency cycle with no remaining declarations is an orphan.
	f.Add([]byte{
		1, 1, 1,
		mutationAbsent, mutationAbsent,
		mutationAbsent, mutationAbsent,
	})
	// A cycle held by one disabled root stays pinned and dormant.
	f.Add([]byte{
		1, 1, 1,
		mutationDisabled, mutationAbsent,
		mutationDisabled, mutationDisabled,
	})
	// Three-node chain: removing the parent keeps the enabled middle root and
	// its transitive dependency.
	f.Add([]byte{
		2,
		1, 0, 0, 1, 0, 0,
		mutationAbsent, mutationEnabled, mutationAbsent,
		mutationAbsent, mutationEnabled, mutationAbsent,
	})
	// Three-node chain: round one activates everything from alpha, then round
	// two disables both present roots so every node is retained but dormant
	// until the re-enable round.
	f.Add([]byte{
		2,
		1, 0, 0, 1, 0, 0,
		mutationEnabled, mutationDisabled, mutationAbsent,
		mutationDisabled, mutationDisabled, mutationAbsent,
	})
	// No mutation at all: the fingerprint matches and the lock replays.
	f.Add([]byte{
		1, 0, 0,
		mutationEnabled, mutationEnabled,
		mutationEnabled, mutationEnabled,
	})

	f.Fuzz(func(t *testing.T, raw []byte) {
		scenario := decodeMutationScenario(raw)
		root := t.TempDir()
		writeInitialWorkspace(t, root, scenario)
		description := scenario.describe()

		retained1 := mutationReachable(scenario.edges, retainedSeeds(scenario.first))
		active1 := mutationReachable(scenario.edges, enabledSeeds(scenario.first))
		plan1 := runLockedMutation(t, root, scenario, scenario.first)
		checkMutationPlan(t, plan1, scenario, scenario.first, retained1, active1,
			fmt.Sprintf("%s first=%v", description, scenario.first))

		writeResolvedLock(t, root, plan1.Resolved)
		retained2 := mutationReachable(scenario.edges, retainedSeeds(scenario.second))
		active2 := mutationReachable(scenario.edges, enabledSeeds(scenario.second))
		plan2 := runLockedMutation(t, root, scenario, scenario.second)
		checkMutationPlan(t, plan2, scenario, scenario.second, retained2, active2,
			fmt.Sprintf("%s second=%v", description, scenario.second))

		writeResolvedLock(t, root, plan2.Resolved)
		third := reenabledStates(scenario.second)
		retained3 := mutationReachable(scenario.edges, retainedSeeds(third))
		active3 := mutationReachable(scenario.edges, enabledSeeds(third))
		plan3 := runLockedMutation(t, root, scenario, third)
		checkMutationPlan(t, plan3, scenario, third, retained3, active3,
			fmt.Sprintf("%s reenabled=%v", description, third))
	})
}

func decodeMutationScenario(raw []byte) mutationScenario {
	cursor := mutationCursor{data: raw}
	nodes := 1 + cursor.next()%len(mutationNames)
	scenario := mutationScenario{nodes: nodes}
	for source := range nodes {
		for target := range nodes {
			if source == target {
				continue
			}
			if cursor.next()%2 != 0 {
				scenario.edges = append(scenario.edges, [2]int{source, target})
			}
		}
	}
	scenario.first = make([]int, nodes)
	scenario.second = make([]int, nodes)
	for index := range nodes {
		scenario.first[index] = cursor.next() % 3
	}
	for index := range nodes {
		scenario.second[index] = cursor.next() % 3
	}
	// A package dropped in the first round has no pin left to mutate, and a
	// locked mutation cannot introduce one.
	retained := mutationReachable(scenario.edges, retainedSeeds(scenario.first))
	for index := range nodes {
		if !retained[index] {
			scenario.second[index] = mutationAbsent
		}
	}
	return scenario
}

func (s mutationScenario) describe() string {
	return fmt.Sprintf("nodes=%d edges=%v", s.nodes, s.edges)
}

type mutationCursor struct {
	data []byte
	pos  int
}

func (c *mutationCursor) next() int {
	if c.pos >= len(c.data) {
		return 0
	}
	value := int(c.data[c.pos])
	c.pos++
	return value
}

// mutationReachable enumerates every simple path from every seed instead of
// reproducing the production traversal.
func mutationReachable(edges [][2]int, seeds map[int]bool) map[int]bool {
	reached := map[int]bool{}
	var walk func(node int, path map[int]bool)
	walk = func(node int, path map[int]bool) {
		if path[node] {
			return
		}
		reached[node] = true
		path[node] = true
		for _, edge := range edges {
			if edge[0] == node {
				walk(edge[1], path)
			}
		}
		delete(path, node)
	}
	for seed := range seeds {
		walk(seed, map[int]bool{})
	}
	return reached
}

func retainedSeeds(states []int) map[int]bool {
	seeds := map[int]bool{}
	for index, state := range states {
		if state != mutationAbsent {
			seeds[index] = true
		}
	}
	return seeds
}

func enabledSeeds(states []int) map[int]bool {
	seeds := map[int]bool{}
	for index, state := range states {
		if state == mutationEnabled {
			seeds[index] = true
		}
	}
	return seeds
}

func reenabledStates(states []int) []int {
	out := make([]int, len(states))
	for index, state := range states {
		if state == mutationAbsent {
			out[index] = mutationAbsent
			continue
		}
		out[index] = mutationEnabled
	}
	return out
}

func writeInitialWorkspace(t *testing.T, root string, scenario mutationScenario) {
	t.Helper()
	initial := make([]int, scenario.nodes)
	for index := range initial {
		initial[index] = mutationEnabled
	}
	m := mutationManifest(initial)
	if err := manifest.Write(root, m); err != nil {
		t.Fatalf("manifest.Write() error = %v", err)
	}
	fingerprint, err := manifest.Fingerprint(m)
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	if err := lockfile.Write(root, initialLock(scenario, fingerprint)); err != nil {
		t.Fatalf("lockfile.Write() error = %v", err)
	}
	if _, err := lockfile.Read(root); err != nil {
		t.Fatalf("lockfile.Read() error = %v", err)
	}
}

func runLockedMutation(
	t *testing.T,
	root string,
	scenario mutationScenario,
	states []int,
) *install.Plan {
	t.Helper()
	if err := manifest.Write(root, mutationManifest(states)); err != nil {
		t.Fatalf("manifest.Write() error = %v", err)
	}
	d, err := manifest.Read(root)
	if err != nil {
		t.Fatalf("manifest.Read() error = %v", err)
	}
	old, err := lockfile.Read(root)
	if err != nil {
		t.Fatalf("lockfile.Read() error = %v", err)
	}
	manifestBefore, err := manifest.Fingerprint(d)
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	lockBefore, err := lockfile.Fingerprint(old)
	if err != nil {
		t.Fatalf("lockfile.Fingerprint() error = %v", err)
	}
	plan, err := install.PlanInstall(
		t.Context(),
		root,
		d,
		old,
		install.Options{Locked: true, RequirementChange: true, Offline: true},
	)
	if err != nil {
		t.Fatalf("PlanInstall() error = %v; %s states=%v", err, scenario.describe(), states)
	}
	manifestAfter, err := manifest.Fingerprint(d)
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	lockAfter, err := lockfile.Fingerprint(old)
	if err != nil {
		t.Fatalf("lockfile.Fingerprint() error = %v", err)
	}
	if manifestBefore != manifestAfter {
		t.Fatalf("PlanInstall() mutated its input manifest; %s states=%v", scenario.describe(), states)
	}
	if lockBefore != lockAfter {
		t.Fatalf("PlanInstall() mutated its input lock; %s states=%v", scenario.describe(), states)
	}
	if got := plan.Resolved.ManifestFingerprint; got != manifestBefore {
		t.Fatalf(
			"resolved lock answers manifest %q, want %q; %s",
			got,
			manifestBefore,
			scenario.describe(),
		)
	}
	return plan
}

func writeResolvedLock(t *testing.T, root string, resolved *lockfile.Document) {
	t.Helper()
	if resolved == nil {
		t.Fatal("PlanInstall() returned a plan without a resolved lock")
	}
	if err := lockfile.Write(root, resolved); err != nil {
		t.Fatalf("lockfile.Write() error = %v", err)
	}
}

func mutationManifest(states []int) *manifest.Document {
	packages := make(map[string]manifest.Requirement)
	for index, state := range states {
		if state == mutationAbsent {
			continue
		}
		requirement := manifest.Requirement{Selector: versionpkg.Any}
		if state == mutationDisabled {
			requirement.Enabled = new(false)
		}
		packages["modrinth:"+mutationNames[index]] = requirement
	}
	return &manifest.Document{
		SchemaVersion: manifest.SchemaVersion,
		Server: manifest.Server{
			Minecraft: versionpkg.Version("1.21.4"),
			Directory: ".",
			Core: manifest.Core{
				Product:        serverpkg.ProductFabric,
				ProductVersion: versionpkg.Version("0.16.9"),
			},
			Packages: map[catalog.Ecosystem]map[string]manifest.Requirement{
				catalog.EcosystemFabric: packages,
			},
		},
	}
}

func initialLock(scenario mutationScenario, fingerprint string) *lockfile.Document {
	incoming := incomingSources(scenario)
	packages := make([]lockfile.Package, 0, scenario.nodes)
	for index := range scenario.nodes {
		name := mutationNames[index]
		sources := make([]catalog.Ref, 0, len(incoming[index]))
		for _, source := range incoming[index] {
			sources = append(sources, catalog.Ref{
				Name:     catalog.Name(mutationNames[source]),
				Provider: catalog.ProviderModrinth,
			})
		}
		packages = append(packages, lockfile.Package{
			Environment: serverpkg.EnvironmentServer,
			Ecosystem:   catalog.EcosystemFabric,
			Artifact:    mutationArtifact(index),
			Requirements: []lockfile.RequirementBinding{{
				Reference: "modrinth:" + name,
				Selector:  versionpkg.Any,
				Enabled:   true,
			}},
			Dependency:   len(incoming[index]) > 0,
			DependencyOf: sources,
		})
	}
	return &lockfile.Document{
		SchemaVersion:       lockfile.SchemaVersion,
		ManifestFingerprint: fingerprint,
		Server: lockfile.Server{
			Directory:   ".",
			Minecraft:   versionpkg.Version("1.21.4"),
			Product:     serverpkg.ProductFabric,
			CoreVersion: versionpkg.Version("0.16.9"),
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderFabric,
				ProjectID: "fabric-loader",
				Version:   versionpkg.Version("0.16.9"),
				Filename:  "fabric-loader-0.16.9.jar",
				Hashes:    map[string]string{"sha256": packageHash(100)},
			},
			Bootstrapper: "fabric",
			Java:         lockfile.Java{Advisory: 21},
		},
		Packages: packages,
	}
}

func incomingSources(scenario mutationScenario) [][]int {
	incoming := make([][]int, scenario.nodes)
	for _, edge := range scenario.edges {
		incoming[edge[1]] = append(incoming[edge[1]], edge[0])
	}
	return incoming
}

func mutationArtifact(index int) catalog.Artifact {
	name := mutationNames[index]
	return catalog.Artifact{
		Provider:  catalog.ProviderModrinth,
		ProjectID: name,
		ReleaseID: name + "-r1",
		FileID:    name + "-f1",
		Version:   versionpkg.Version("1.0.0"),
		Filename:  name + ".jar",
		Hashes:    map[string]string{"sha256": packageHash(index)},
	}
}

func packageHash(index int) string {
	return fmt.Sprintf("%064x", index+1)
}

func checkMutationPlan(
	t *testing.T,
	plan *install.Plan,
	scenario mutationScenario,
	states []int,
	retained map[int]bool,
	active map[int]bool,
	description string,
) {
	t.Helper()
	if plan == nil || plan.Resolved == nil {
		t.Fatalf("PlanInstall() returned no resolved lock; %s", description)
	}
	declared := declaredReferences(states)
	seen := make(map[string]bool, len(plan.Resolved.Packages))
	if len(plan.Resolved.Packages) != len(retained) {
		t.Fatalf(
			"retained packages = %d, want %d; %s states=%v",
			len(plan.Resolved.Packages),
			len(retained),
			description,
			states,
		)
	}
	for _, pkg := range plan.Resolved.Packages {
		index := mutationIndex(pkg.ProjectID)
		if index < 0 {
			t.Fatalf("selected unknown project %q; %s", pkg.ProjectID, description)
		}
		if !retained[index] {
			t.Fatalf("selected %s although it is not reachable; %s states=%v", pkg.ProjectID, description, states)
		}
		if seen[pkg.ProjectID] {
			t.Fatalf("selected %s more than once; %s", pkg.ProjectID, description)
		}
		seen[pkg.ProjectID] = true
		if pkg.Environment != serverpkg.EnvironmentServer ||
			pkg.Ecosystem != catalog.EcosystemFabric {
			t.Fatalf(
				"selected %s as %s/%s; %s",
				pkg.ProjectID,
				pkg.Environment,
				pkg.Ecosystem,
				description,
			)
		}
		if got, want := pkg.Enabled(), active[index]; got != want {
			t.Fatalf(
				"selected %s Enabled() = %v, want %v; requirements=%#v; %s states=%v",
				pkg.ProjectID,
				got,
				want,
				pkg.Requirements,
				description,
				states,
			)
		}
		if !reflect.DeepEqual(pkg.Artifact, mutationArtifact(index)) {
			t.Fatalf("selected %s changed its artifact pin: %#v; %s", pkg.ProjectID, pkg.Artifact, description)
		}
		parents := retainedParents(scenario, retained, index)
		if want := mutationSources(parents); !sameSources(pkg.DependencyOf, want) {
			t.Fatalf(
				"selected %s DependencyOf = %#v, want %#v; %s states=%v",
				pkg.ProjectID,
				pkg.DependencyOf,
				want,
				description,
				states,
			)
		}
		wantDependency := false
		for _, parent := range parents {
			wantDependency = wantDependency || active[parent]
		}
		if pkg.Dependency != wantDependency {
			t.Fatalf(
				"selected %s Dependency = %v, want %v; parents=%v; %s states=%v",
				pkg.ProjectID,
				pkg.Dependency,
				wantDependency,
				parents,
				description,
				states,
			)
		}
		ownReference := "modrinth:" + pkg.ProjectID
		ownBindings := 0
		for _, binding := range pkg.Requirements {
			if !declared[binding.Reference] {
				t.Fatalf(
					"selected %s has a stale binding %#v; %s states=%v",
					pkg.ProjectID,
					binding,
					description,
					states,
				)
			}
			if binding.Reference != ownReference {
				continue
			}
			ownBindings++
			if binding.Selector != versionpkg.Any || binding.FilePattern != "" {
				t.Fatalf(
					"selected %s direct binding = %#v; %s",
					pkg.ProjectID,
					binding,
					description,
				)
			}
			wantEnabled := states[index] == mutationEnabled
			if binding.Enabled != wantEnabled {
				t.Fatalf(
					"selected %s direct binding Enabled = %v, want %v; %s states=%v",
					pkg.ProjectID,
					binding.Enabled,
					wantEnabled,
					description,
					states,
				)
			}
		}
		if states[index] == mutationAbsent {
			if ownBindings != 0 {
				t.Fatalf(
					"selected %s kept the removed direct binding; requirements=%#v; %s",
					pkg.ProjectID,
					pkg.Requirements,
					description,
				)
			}
			continue
		}
		if ownBindings != 1 {
			t.Fatalf(
				"selected %s direct bindings = %d, want 1; requirements=%#v; %s states=%v",
				pkg.ProjectID,
				ownBindings,
				pkg.Requirements,
				description,
				states,
			)
		}
	}
	for index := range scenario.nodes {
		if retained[index] && !seen[mutationNames[index]] {
			t.Fatalf("retained package %s is missing; %s states=%v", mutationNames[index], description, states)
		}
	}
}

// retainedParents lists the retained packages whose dependency edges point at
// node. It is the independent expectation for the provenance a locked mutation
// must persist.
func retainedParents(scenario mutationScenario, retained map[int]bool, node int) []int {
	var parents []int
	for _, edge := range scenario.edges {
		if edge[1] == node && retained[edge[0]] {
			parents = append(parents, edge[0])
		}
	}
	return parents
}

func mutationSources(parents []int) []catalog.Ref {
	sources := make([]catalog.Ref, 0, len(parents))
	for _, parent := range parents {
		sources = append(sources, catalog.Ref{
			Name:     catalog.Name(mutationNames[parent]),
			Provider: catalog.ProviderModrinth,
		})
	}
	return sources
}

func sameSources(actual, want []catalog.Ref) bool {
	return len(actual) == len(want) &&
		reflect.DeepEqual(mutationSourceSet(actual), mutationSourceSet(want))
}

func mutationSourceSet(sources []catalog.Ref) map[catalog.Ref]int {
	set := make(map[catalog.Ref]int, len(sources))
	for _, source := range sources {
		set[source]++
	}
	return set
}

func declaredReferences(states []int) map[string]bool {
	declared := make(map[string]bool, len(states))
	for index, state := range states {
		if state != mutationAbsent {
			declared["modrinth:"+mutationNames[index]] = true
		}
	}
	return declared
}

func mutationIndex(name string) int {
	for index, candidate := range mutationNames {
		if candidate == name {
			return index
		}
	}
	return -1
}
