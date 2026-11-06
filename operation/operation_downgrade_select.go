package operation

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// downgradeTarget is one direct manifest root pinned to a chosen older build.
type downgradeTarget struct {
	Environment serverpkg.Environment
	Ecosystem   catalog.Ecosystem
	Reference   string
	Requirement document.Requirement
	Provider    catalog.Provider
	Project     string
	Previous    *document.Package
	Version     versionpkg.Version
	Candidate   upstream.Candidate
}

// downgradePreviousKey identifies one locked upstream package. Two manifest
// references that share it name the same package and cannot be downgraded
// separately.
type downgradePreviousKey struct {
	Provider    catalog.Provider
	Project     string
	Environment serverpkg.Environment
	Ecosystem   catalog.Ecosystem
}

// selectDowngradeTargets resolves each argument to an enabled direct manifest
// root and picks the older build the requested step count names.
func (s *Service) selectDowngradeTargets(
	ctx context.Context,
	d *document.Manifest,
	old *document.Lock,
	args []string,
	ecosystem catalog.Ecosystem,
	steps int,
) ([]downgradeTarget, error) {
	ecosystem = selectedEcosystem(d, ecosystem)
	if ecosystem == catalog.EcosystemUnspecified {
		return nil, fmt.Errorf("specify a package ecosystem supported by the declared server")
	}
	targets := make([]downgradeTarget, 0, len(args))
	seen := map[string]bool{}
	seenPrevious := map[downgradePreviousKey]string{}
	for _, raw := range args {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		request, err := input.Parse(raw)
		if err != nil {
			return nil, err
		}
		if strings.Contains(raw, "@") ||
			(request.Version != "" && request.Version != versionpkg.Any) {
			return nil, fmt.Errorf("downgrade targets name packages, not versions: %s", raw)
		}
		ref, _, err := commandReference(d, old, raw, ecosystem)
		if err != nil {
			return nil, err
		}
		if seen[ref] {
			return nil, fmt.Errorf("package %s is named more than once", ref)
		}
		seen[ref] = true
		environment, requirement, declared := downgradeRootRequirement(d, ecosystem, ref)
		if !declared {
			return nil, fmt.Errorf(
				"package %s is not a direct requirement of ecosystem %s; only direct manifest packages can be downgraded",
				ref,
				ecosystem,
			)
		}
		if !requirement.IsEnabled() {
			return nil, fmt.Errorf("package %s is disabled in the manifest; enable it before downgrading", ref)
		}
		previous := downgradePreviousPackage(old, environment, ecosystem, ref)
		if previous == nil {
			return nil, fmt.Errorf("lock has no package for manifest root %s; run lucy install first", ref)
		}
		identity := downgradePreviousKey{
			Provider:    previous.Provider,
			Project:     previous.ProjectID,
			Environment: previous.Environment,
			Ecosystem:   previous.Ecosystem,
		}
		if prior, exists := seenPrevious[identity]; exists {
			return nil, fmt.Errorf(
				"packages %s and %s are the same locked upstream package %s:%s; downgrade it once",
				prior,
				ref,
				previous.Provider,
				previous.ProjectID,
			)
		}
		seenPrevious[identity] = ref
		provider, project, err := document.ParseReference(ref)
		if err != nil {
			return nil, err
		}
		version, candidate, err := s.selectDowngradeVersion(
			ctx,
			old,
			ref,
			ecosystem,
			requirement,
			previous,
			steps,
		)
		if err != nil {
			return nil, err
		}
		targets = append(targets, downgradeTarget{
			Environment: environment,
			Ecosystem:   ecosystem,
			Reference:   ref,
			Requirement: requirement,
			Provider:    provider,
			Project:     project,
			Previous:    previous,
			Version:     version,
			Candidate:   candidate,
		})
	}
	return targets, nil
}

func downgradeRootRequirement(
	d *document.Manifest,
	ecosystem catalog.Ecosystem,
	ref string,
) (serverpkg.Environment, document.Requirement, bool) {
	if ecosystem == catalog.EcosystemMCDR {
		if d.MCDR == nil {
			return serverpkg.EnvironmentMCDR, document.Requirement{}, false
		}
		requirement, ok := d.MCDR.Packages[ref]
		return serverpkg.EnvironmentMCDR, requirement, ok
	}
	requirement, ok := d.Server.Packages[ecosystem][ref]
	return serverpkg.EnvironmentServer, requirement, ok
}

func downgradePreviousPackage(
	old *document.Lock,
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	ref string,
) *document.Package {
	for i := range old.Packages {
		p := &old.Packages[i]
		if p.Environment != environment || p.Ecosystem != ecosystem {
			continue
		}
		for _, binding := range p.Requirements {
			if binding.Reference == ref {
				return p
			}
		}
	}
	return nil
}

// selectDowngradeVersion picks the older version label at the requested step
// and one deterministic build inside that label. The locked version is the
// baseline; upstream ordering only decides which older labels exist.
func (s *Service) selectDowngradeVersion(
	ctx context.Context,
	old *document.Lock,
	ref string,
	ecosystem catalog.Ecosystem,
	requirement document.Requirement,
	previous *document.Package,
	steps int,
) (versionpkg.Version, upstream.Candidate, error) {
	dialect := upgradeArtifactDialect(ecosystem)
	if !dialect.Valid() || dialect == versionpkg.DialectOpaque {
		return "", upstream.Candidate{}, fmt.Errorf(
			"%s package versions have no ordering; cannot downgrade %s",
			ecosystem,
			ref,
		)
	}
	locked, err := versionpkg.ParseVersion(previous.Version, dialect)
	if err != nil {
		return "", upstream.Candidate{}, fmt.Errorf(
			"locked version %s of %s is not a comparable %s version: %w",
			previous.Version,
			ref,
			dialect,
			err,
		)
	}
	provider, project, err := document.ParseReference(ref)
	if err != nil {
		return "", upstream.Candidate{}, err
	}
	registry, ok := provider.Registry()
	if !ok {
		return "", upstream.Candidate{}, fmt.Errorf("provider %s has no package catalogue", provider)
	}
	list, err := s.source.Candidates(
		ctx,
		registry,
		project,
		string(old.Server.Minecraft),
		ecosystem,
	)
	if err != nil {
		return "", upstream.Candidate{}, fmt.Errorf("list %s builds: %w", ref, err)
	}
	records := map[versionpkg.Version]downgradeVersionRecord{}
	for _, candidate := range list {
		if requirement.FilePattern != "" {
			match, err := filepath.Match(requirement.FilePattern, candidate.Filename)
			if err != nil {
				return "", upstream.Candidate{}, fmt.Errorf(
					"invalid file pattern %q for %s: %w",
					requirement.FilePattern,
					ref,
					err,
				)
			}
			if !match {
				continue
			}
		} else if !candidate.PrimaryFile {
			continue
		}
		if record, seen := records[candidate.Version]; seen {
			if betterDowngradeCandidate(candidate, record.best) {
				record.best = candidate
				records[candidate.Version] = record
			}
			continue
		}
		candidateVersion, err := versionpkg.ParseVersion(candidate.Version, dialect)
		if err != nil {
			s.logger.Debug(
				"downgrade: candidate version is not comparable",
				"package", ref,
				"version", candidate.Version,
				"error", err,
			)
			continue
		}
		comparison, comparable := versionpkg.Compare(candidateVersion, locked)
		if !comparable || comparison >= 0 {
			continue
		}
		records[candidate.Version] = downgradeVersionRecord{
			parsed: candidateVersion,
			best:   candidate,
		}
	}
	labels := make([]versionpkg.Version, 0, len(records))
	for label := range records {
		labels = append(labels, label)
	}
	slices.SortFunc(labels, func(a, b versionpkg.Version) int {
		comparison, comparable := versionpkg.Compare(
			records[a].parsed,
			records[b].parsed,
		)
		if comparable && comparison != 0 {
			return -comparison
		}
		return strings.Compare(string(a), string(b))
	})
	if steps > len(labels) {
		return "", upstream.Candidate{}, fmt.Errorf(
			"cannot downgrade %s from %s by %d step(s): only %d older version(s) available for Minecraft %s",
			ref,
			previous.Version,
			steps,
			len(labels),
			old.Server.Minecraft,
		)
	}
	selected := labels[steps-1]
	return selected, records[selected].best, nil
}

type downgradeVersionRecord struct {
	parsed versionpkg.ResolvableVersion
	best   upstream.Candidate
}

// betterDowngradeCandidate reports whether a wins over b for the same version
// label: the newest artifact, then stable lexical identity as a tie breaker.
func betterDowngradeCandidate(a, b upstream.Candidate) bool {
	if n := a.PublishedAt.Compare(b.PublishedAt); n != 0 {
		return n > 0
	}
	if n := strings.Compare(a.ReleaseID, b.ReleaseID); n != 0 {
		return n < 0
	}
	if n := strings.Compare(a.FileID, b.FileID); n != 0 {
		return n < 0
	}
	return a.Filename < b.Filename
}

type downgradeCandidateKey struct {
	Registry  catalog.Registry
	Ecosystem catalog.Ecosystem
	Project   string
}

type downgradeCandidateChoice struct {
	Candidate upstream.Candidate
	Reference string
}

// downgradeCandidateSource restricts each target project to its chosen build,
// under both the manifest slug and the canonical upstream identity. Another
// version of the same project can never answer the downgrade request.
func downgradeCandidateSource(
	next resolve.CandidateSource,
	targets []downgradeTarget,
) (resolve.CandidateSource, error) {
	chosen := map[downgradeCandidateKey]downgradeCandidateChoice{}
	for _, target := range targets {
		registry, ok := target.Provider.Registry()
		if !ok {
			return nil, fmt.Errorf("provider %s has no package catalogue", target.Provider)
		}
		var keys []downgradeCandidateKey
		if target.Candidate.ProjectID != "" {
			keys = append(keys, downgradeCandidateKey{
				Registry:  registry,
				Ecosystem: target.Ecosystem,
				Project:   target.Candidate.ProjectID,
			})
		}
		if target.Project != "" && target.Project != target.Candidate.ProjectID {
			keys = append(keys, downgradeCandidateKey{
				Registry:  registry,
				Ecosystem: target.Ecosystem,
				Project:   target.Project,
			})
		}
		if target.Previous != nil &&
			target.Previous.ProjectID != "" &&
			target.Previous.ProjectID != target.Candidate.ProjectID &&
			target.Previous.ProjectID != target.Project {
			keys = append(keys, downgradeCandidateKey{
				Registry:  registry,
				Ecosystem: target.Ecosystem,
				Project:   target.Previous.ProjectID,
			})
		}
		for _, key := range keys {
			if prior, exists := chosen[key]; exists && !sameDowngradeCandidate(prior.Candidate, target.Candidate) {
				return nil, fmt.Errorf(
					"packages %s and %s resolve to the same upstream project with different builds",
					prior.Reference,
					target.Reference,
				)
			}
			chosen[key] = downgradeCandidateChoice{
				Candidate: target.Candidate,
				Reference: target.Reference,
			}
		}
	}
	return func(
		ctx context.Context,
		registry catalog.Registry,
		project, minecraft string,
		ecosystem catalog.Ecosystem,
	) ([]upstream.Candidate, error) {
		if choice, ok := chosen[downgradeCandidateKey{
			Registry:  registry,
			Ecosystem: ecosystem,
			Project:   project,
		}]; ok {
			return []upstream.Candidate{choice.Candidate}, nil
		}
		return next(ctx, registry, project, minecraft, ecosystem)
	}, nil
}

func sameDowngradeCandidate(a, b upstream.Candidate) bool {
	return a.Provider == b.Provider &&
		a.ProjectID == b.ProjectID &&
		a.ReleaseID == b.ReleaseID &&
		a.FileID == b.FileID &&
		a.Version == b.Version &&
		a.Filename == b.Filename
}

// verifyDowngradeSelection ensures resolution committed exactly the build the
// selection chose, not merely another build carrying the same version label.
func verifyDowngradeSelection(packages []document.Package, targets []downgradeTarget) error {
	for _, target := range targets {
		selected := downgradeSelectedPackage(packages, target)
		if selected == nil {
			return fmt.Errorf("resolution did not select %s", target.Reference)
		}
		candidate := target.Candidate
		if selected.Version != candidate.Version ||
			selected.Provider != candidate.Provider ||
			selected.ProjectID != candidate.ProjectID ||
			selected.ReleaseID != candidate.ReleaseID ||
			selected.FileID != candidate.FileID {
			return fmt.Errorf(
				"resolution selected %s %s (%s:%s/%s) instead of requested %s (%s:%s/%s)",
				target.Reference,
				selected.Version,
				selected.Provider,
				selected.ReleaseID,
				selected.FileID,
				candidate.Version,
				candidate.Provider,
				candidate.ReleaseID,
				candidate.FileID,
			)
		}
	}
	return nil
}

func downgradeSelectedPackage(packages []document.Package, target downgradeTarget) *document.Package {
	for i := range packages {
		p := &packages[i]
		if p.Environment != target.Environment || p.Ecosystem != target.Ecosystem {
			continue
		}
		for _, binding := range p.Requirements {
			if binding.Reference == target.Reference {
				return p
			}
		}
	}
	return nil
}

func setDowngradeSelector(
	d *document.Manifest,
	target downgradeTarget,
	selector versionpkg.Version,
) error {
	return setRootSelector(d, upgradeRoot{
		Environment: target.Environment,
		Ecosystem:   target.Ecosystem,
		Reference:   target.Reference,
	}, selector)
}
