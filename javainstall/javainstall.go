// Package javainstall installs a Java runtime through the most native method
// the host offers. The order is a Java version manager the user already runs,
// then the system package manager, then a vendor repository the user consented
// to, and last a per-user download. Lucy never changes the default Java of the
// user or the system.
package javainstall

import (
	"cmp"
	"context"
	"slices"

	"github.com/mclucy/lucy/java"
)

// Request names the Java runtime to install.
type Request struct {
	Major        int
	Distribution java.Distribution
}

// Method names the mechanism that performs an installation.
type Method string

const (
	MethodSDKMAN   Method = "sdkman"
	MethodMise     Method = "mise"
	MethodAsdf     Method = "asdf"
	MethodHomebrew Method = "brew"
	MethodScoop    Method = "scoop"
	MethodWinget   Method = "winget"
	MethodApt      Method = "apt"
	MethodDnf      Method = "dnf"
	MethodZypper   Method = "zypper"
	MethodPacman   Method = "pacman"
	MethodApk      Method = "apk"
	MethodDownload Method = "download"
)

// Elevation names who obtains the rights a step needs.
type Elevation int

const (
	// ElevationNone runs the step as the current user.
	ElevationNone Elevation = iota
	// ElevationRoot runs the step as root: directly when Lucy already runs as
	// root, otherwise through sudo.
	ElevationRoot
	// ElevationSelf runs the step as the current user. The tool asks for rights
	// or agreements itself, so the step needs an interactive terminal.
	ElevationSelf
)

// Step is one command of a plan.
type Step struct {
	Args      []string
	Elevation Elevation
	// Input is fed to the standard input when set. An empty Input connects the
	// terminal, so the tool can prompt.
	Input string
}

// Repository is a vendor package repository that a plan adds before its steps.
type Repository struct {
	// Name is shown to the user, for example "Eclipse Adoptium apt repository".
	Name string
	// URL is the repository location shown to the user.
	URL string
	// Key is the signing key that the repository files reference. It is nil
	// when the package manager verifies the source itself.
	Key *RepositoryKey
	// Files are written as root after the key.
	Files []RepositoryFile
	// Steps run after the files, for example to refresh the package index.
	Steps []Step
}

// RepositoryKey is an OpenPGP key that Lucy downloads and verifies against a
// pinned fingerprint before it writes the key.
type RepositoryKey struct {
	URL string
	// Fingerprint is the primary key fingerprint in upper-case hexadecimal
	// without spaces.
	Fingerprint string
	// Path is where the verified key is written as root.
	Path string
	// Armored writes the key ASCII-armored; otherwise the key is written in
	// binary form.
	Armored bool
}

// RepositoryFile is one file a repository plan writes as root.
type RepositoryFile struct {
	Path    string
	Content string
}

// Plan is one way to install the requested Java.
type Plan struct {
	Method Method
	// Package is the package, cask, candidate, or archive that the plan
	// installs.
	Package string
	// Distribution is the build the plan installs. It is the unspecified
	// distribution for unbranded builds.
	Distribution java.Distribution
	Major        int
	// Repository is added before the steps. A plan with a repository needs
	// Policy.AllowRepository.
	Repository *Repository
	Steps      []Step
	// KeepDefault restores the system default Java captured before the steps,
	// because package execution can move it before a later failure or
	// cancellation. A step that exits from an unexpected signal blocks the
	// restore because its child cannot be confirmed stopped.
	KeepDefault bool
	// root is the elevation fact captured while planning, so Commands renders
	// the same sudo prefixes that execution uses.
	root bool
	// managedRoot is the per-user runtime directory captured while planning a
	// download, so Commands and execution name the same destination.
	managedRoot string
	download    *download
}

// SkipReason tells why a plan that could install the request is not usable.
type SkipReason int

const (
	// SkipNeedsRoot means the plan needs root and Lucy can neither run as root
	// nor use sudo.
	SkipNeedsRoot SkipReason = iota + 1
	// SkipNeedsTerminal means the tool prompts for rights or agreements and no
	// interactive terminal is available.
	SkipNeedsTerminal
	// SkipNeedsRepositoryConsent means the plan adds a vendor repository and
	// the user did not consent.
	SkipNeedsRepositoryConsent
)

// Skipped is a plan that could install the request but is not usable on this
// run.
type Skipped struct {
	Plan   Plan
	Reason SkipReason
}

// manager is one package manager or Java version manager.
// Managers are stateless; the Host carries the invocation's facts.
type manager interface {
	method() Method
	// system reports that the manager installs for the whole system rather
	// than for the current user.
	system() bool
	// ownsJava reports that the manager already installed a Java runtime on
	// this host.
	ownsJava(ctx context.Context, host *Host) bool
	// plans returns the ways the manager can install req. It returns nil when
	// the manager is absent or offers no matching package.
	plans(ctx context.Context, req Request, host *Host) ([]Plan, error)
}

func managers() []manager {
	return []manager{
		sdkman{},
		mise{},
		asdf{},
		homebrew{},
		scoop{},
		apt{},
		dnf{},
		zypper{},
		pacman{},
		apk{},
		winget{},
	}
}

const (
	tierOwningUser = iota
	tierUser
	tierSystem
	tierRepository
	tierDownload
)

// Plans lists the usable ways to install req, most native first, and the
// plans that this run cannot use. A manager that fails to answer is skipped,
// because another method can still install the request.
func (h *Host) Plans(
	ctx context.Context,
	req Request,
	policy Policy,
) ([]Plan, []Skipped, error) {
	type ranked struct {
		plan Plan
		tier int
	}
	var candidates []ranked
	for _, m := range managers() {
		plans, err := m.plans(ctx, req, h)
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		if err != nil {
			h.logger.Debug(
				"java install method unavailable",
				"method", m.method(),
				"reason", err,
			)
			continue
		}
		tier := tierUser
		switch {
		case m.system():
			tier = tierSystem
		case m.ownsJava(ctx, h):
			tier = tierOwningUser
		}
		for _, plan := range plans {
			plan.root = h.isRoot()
			planTier := tier
			if plan.Repository != nil {
				planTier = tierRepository
			}
			candidates = append(candidates, ranked{plan: plan, tier: planTier})
		}
	}
	download, err := h.downloadPlan(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		h.logger.Debug("java download unavailable", "reason", err)
	} else if download != nil {
		candidates = append(candidates, ranked{plan: *download, tier: tierDownload})
	}
	slices.SortStableFunc(candidates, func(a, b ranked) int {
		return cmp.Compare(a.tier, b.tier)
	})
	var usable []Plan
	var skipped []Skipped
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		plan := candidate.plan
		if plan.Major != req.Major || !req.Distribution.Accepts(plan.Distribution) {
			h.logger.Debug(
				"java install plan rejected",
				"method", plan.Method,
				"package", plan.Package,
				"reason", "plan does not install the requested Java",
			)
			continue
		}
		reason, err := skipReason(ctx, plan, policy, h)
		if err != nil {
			return nil, nil, err
		}
		if reason != 0 {
			skipped = append(skipped, Skipped{Plan: plan, Reason: reason})
			continue
		}
		usable = append(usable, plan)
	}
	return usable, skipped, ctx.Err()
}

func skipReason(
	ctx context.Context,
	plan Plan,
	policy Policy,
	host *Host,
) (SkipReason, error) {
	if plan.Repository != nil && !policy.AllowRepository {
		return SkipNeedsRepositoryConsent, nil
	}
	for _, step := range plan.allSteps() {
		switch step.Elevation {
		case ElevationRoot:
			available, err := host.rootAvailable(ctx, policy)
			if err != nil {
				return 0, err
			}
			if !available {
				return SkipNeedsRoot, nil
			}
		case ElevationSelf:
			if !policy.Interactive {
				return SkipNeedsTerminal, nil
			}
		}
	}
	if plan.Repository != nil && (plan.Repository.Key != nil || len(plan.Repository.Files) > 0) {
		available, err := host.rootAvailable(ctx, policy)
		if err != nil {
			return 0, err
		}
		if !available {
			return SkipNeedsRoot, nil
		}
	}
	return 0, nil
}

// allSteps returns the repository steps followed by the install steps.
func (p Plan) allSteps() []Step {
	if p.Repository == nil {
		return p.Steps
	}
	return append(slices.Clone(p.Repository.Steps), p.Steps...)
}
