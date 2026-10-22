package javaruntime

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/internal/toolchain"
	"github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
)

// Source names how a Java runtime was selected.
type Source string

const (
	// SourceLocal means the local settings select the runtime.
	SourceLocal Source = "local"
	// SourceInstalled means Lucy picked the runtime among the installed ones.
	SourceInstalled Source = "installed"
)

// Runtime is one installed Java runtime.
type Runtime struct {
	Home         string            `json:"home"`
	Executable   string            `json:"executable"`
	Version      string            `json:"version"`
	Major        int               `json:"major"`
	Distribution java.Distribution `json:"distribution,omitempty"`
	Implementor  string            `json:"implementor,omitempty"`
}

// Build names the build of r for people, for example "Eclipse Temurin" or
// "Homebrew".
func (r *Runtime) Build() string {
	if r.Distribution != java.DistributionAny {
		return r.Distribution.Title()
	}
	if r.Implementor != "" {
		return r.Implementor
	}
	return "unbranded"
}

// Check reports the Java runtime that a server runs on and how the runtime
// misses the requirement of the server.
type Check struct {
	// Runtime is nil when no usable runtime exists.
	Runtime *Runtime `json:"runtime,omitempty"`
	Source  Source   `json:"source,omitempty"`
	// LocalHome is the home that the local settings select, also when it is
	// not usable.
	LocalHome string `json:"local_home,omitempty"`
	// Target is the major version that meets the requirement.
	Target       int               `json:"target"`
	Distribution java.Distribution `json:"distribution,omitempty"`
	// Problems describe each way the selection misses the requirement.
	Problems []string `json:"problems,omitempty"`
}

// Satisfied reports whether a runtime exists and meets the requirement.
func (c *Check) Satisfied() bool {
	return c != nil && c.Runtime != nil && len(c.Problems) == 0
}

// Executable returns the java executable of the selected runtime, or "" when
// no runtime is usable.
func (c *Check) Executable() string {
	if c == nil || c.Runtime == nil {
		return ""
	}
	return c.Runtime.Executable
}

// Select chooses the Java runtime that the server in root runs on. The local
// settings take precedence; an unusable local selection is a problem, never a
// reason to fall back to another runtime. Otherwise Select picks the
// installed runtime that misses the fewest constraints, preferring the
// oldest major version that meets the Mojang advisory.
func Select(ctx context.Context, root string, req Requirement) (*Check, error) {
	check := &Check{
		Target:       req.Target(),
		Distribution: req.Distribution,
	}
	local, err := manifest.ReadLocal(root)
	if err != nil {
		return nil, err
	}
	if home := local.JavaHome(root); home != "" {
		check.Source = SourceLocal
		check.LocalHome = home
		installation, err := toolchain.ProbeJava(ctx, home)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			check.Problems = append(check.Problems, fmt.Sprintf(
				"%s selects %s, which is not a usable Java runtime: %v",
				manifest.LocalFilename,
				home,
				err,
			))
			return check, nil
		}
		check.Runtime = runtimeOf(installation)
		check.Problems = append(conflicts(req), req.Misses(check.Runtime)...)
		log.Debug(
			"java selected",
			"home", check.Runtime.Home,
			"major", check.Runtime.Major,
			"reason", "local settings select it",
		)
		return check, nil
	}
	runtimes, err := Installed(ctx)
	if err != nil {
		return nil, err
	}
	if len(runtimes) == 0 {
		check.Problems = append(check.Problems, "no Java runtime is installed")
		return check, nil
	}
	check.Source = SourceInstalled
	check.Runtime = Choose(req, runtimes)
	check.Problems = append(conflicts(req), req.Misses(check.Runtime)...)
	log.Debug(
		"java selected",
		"home", check.Runtime.Home,
		"major", check.Runtime.Major,
		"problems", len(check.Problems),
		"reason", "closest installed runtime to the requirement",
	)
	return check, nil
}

// Installed lists the usable Java runtimes on this machine.
func Installed(ctx context.Context) ([]Runtime, error) {
	inventory, err := toolchain.Discover(ctx)
	if err != nil {
		return nil, err
	}
	for _, problem := range inventory.Problems {
		log.Debug("java candidate unusable", "reason", problem)
	}
	runtimes := make([]Runtime, 0, len(inventory.Java))
	for _, installation := range inventory.Java {
		runtimes = append(runtimes, *runtimeOf(installation))
	}
	return runtimes, nil
}

// Choose returns the runtime that misses the fewest parts of req. Among
// equal runtimes it prefers one that meets the Mojang advisory, then the
// oldest major version, then the newest update. runtimes must not be empty.
func Choose(req Requirement, runtimes []Runtime) *Runtime {
	ranked := slices.Clone(runtimes)
	slices.SortStableFunc(ranked, func(a, b Runtime) int {
		return cmp.Or(
			cmp.Compare(len(req.Misses(&a)), len(req.Misses(&b))),
			compareBool(a.Major < req.Advisory, b.Major < req.Advisory),
			cmp.Compare(a.Major, b.Major),
			compareVersions(b.Version, a.Version),
			cmp.Compare(a.Home, b.Home),
		)
	})
	return &ranked[0]
}

// conflicts reports a requirement that no Java version meets.
func conflicts(req Requirement) []string {
	if _, ok := req.Lowest(); ok {
		return nil
	}
	sources := make([]string, len(req.Constraints))
	for i, c := range req.Constraints {
		sources[i] = c.Source + " (" + c.Expression + ")"
	}
	return []string{"no Java version meets every requirement: " + strings.Join(sources, ", ")}
}

// Misses describes each way runtime misses r. An empty result means runtime
// meets r.
func (r Requirement) Misses(runtime *Runtime) []string {
	var found []string
	for _, c := range r.Unmet(runtime.Major) {
		found = append(found, c.miss(runtime.Major))
	}
	if !r.Distribution.Accepts(runtime.Distribution) {
		found = append(found, fmt.Sprintf(
			"the Java runtime at %s is a %s build, not %s, which %s pins",
			runtime.Home,
			runtime.Build(),
			r.Distribution.Title(),
			manifest.Filename,
		))
	}
	return found
}

func runtimeOf(installation toolchain.JavaInstallation) *Runtime {
	return &Runtime{
		Home:         installation.Home,
		Executable:   installation.Java,
		Version:      installation.Version,
		Major:        installation.Major,
		Distribution: installation.Distribution,
		Implementor:  installation.Implementor,
	}
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

// compareVersions orders Java version strings such as "21.0.5" and "21.0.13"
// by their numeric parts.
func compareVersions(a, b string) int {
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r < '0' || r > '9' })
	}
	left, right := split(a), split(b)
	for i := range min(len(left), len(right)) {
		l, _ := strconv.Atoi(left[i])
		r, _ := strconv.Atoi(right[i])
		if c := cmp.Compare(l, r); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(left), len(right))
}
