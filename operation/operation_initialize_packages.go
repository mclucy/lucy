package operation

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/workspace"
)

// packageAdopter recognizes installed packages against the servers and
// ecosystems this workspace already loads. The file is the ownership unit:
// observation reports one entry per descriptor, so several entries can name
// one file, and adoption claims that path once. A manifest requirement names
// one project, so two different files that identify the same project cannot
// both be managed: the key would record only the last file while the receipt
// and fingerprint claim both. Conflicting projects stay unmanaged.
type packageAdopter struct {
	service  *Service
	root     string
	manifest *documentpkg.Manifest
	observed *workspace.Workspace
	served   map[catalog.Ecosystem]struct{}
	mappers  []packageMapper

	packages  []install.AdoptedPackage
	releases  []install.AdoptedRelease
	index     map[string]int
	claims    map[string]string
	conflicts map[string]bool
}

// adoptionProposal is one file's probe result. Probing reads the file and its
// upstream identity without mutating the adopter, so probes can run in
// parallel and apply in a deterministic order.
type adoptionProposal struct {
	pkg       workspace.DiscoveredPackage
	ecosystem catalog.Ecosystem
	key       string
	reference string
	packageID string
	sha256    string
	mode      uint32
	candidate upstream.Candidate
	matched   bool
	fatal     error
	warnings  []error
}

// packageMapper identifies installed packages by content hash for one
// provider.
type packageMapper struct {
	provider catalog.Provider
	release  upstream.ArtifactReleaseMapper
}

// adoptInstalledPackages adds requirements for the packages discovery
// identified and returns the ownership claims installation must record,
// together with the releases resolution selects for them.
func (s *Service) adoptInstalledPackages(
	ctx context.Context,
	root string,
	manifest *documentpkg.Manifest,
	observed *workspace.Workspace,
) (packages []install.AdoptedPackage, releases []install.AdoptedRelease, inspected int, err error) {
	mappers, err := s.packageMappers()
	if err != nil {
		return nil, nil, 0, err
	}
	adopter := &packageAdopter{
		service:   s,
		root:      root,
		manifest:  manifest,
		observed:  observed,
		served:    servedEcosystemSet(manifest, observed),
		mappers:   mappers,
		index:     map[string]int{},
		claims:    map[string]string{},
		conflicts: map[string]bool{},
	}
	var discovered []workspace.DiscoveredPackage
	if observed != nil {
		discovered = observed.Packages
	}
	unique := uniquePackages(discovered, adopter.served)
	proposals := make([]adoptionProposal, len(unique))
	limit := make(chan struct{}, packageProbeLimit)
	var wg sync.WaitGroup
	for i, p := range unique {
		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			proposals[i] = adopter.probe(ctx, p)
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, nil, 0, err
	}
	for _, proposal := range proposals {
		if proposal.fatal != nil {
			return nil, nil, 0, proposal.fatal
		}
		adopter.apply(proposal)
	}
	packages = slices.DeleteFunc(
		adopter.packages,
		func(pkg install.AdoptedPackage) bool { return pkg.Path == "" },
	)
	releases = slices.DeleteFunc(
		adopter.releases,
		func(release install.AdoptedRelease) bool { return release.Reference == "" },
	)
	s.logger.Debug(
		"recognized existing packages",
		"count", len(packages),
		"present", len(discovered),
	)
	return packages, releases, len(unique), nil
}

// uniquePackages keeps one entry per file, preferring an ecosystem the server
// serves. Observation reports one entry per descriptor, so a jar carrying
// descriptors for several loaders would otherwise be claimed several times.
func uniquePackages(
	discovered []workspace.DiscoveredPackage,
	served map[catalog.Ecosystem]struct{},
) []workspace.DiscoveredPackage {
	positions := make(map[string]int, len(discovered))
	unique := make([]workspace.DiscoveredPackage, 0, len(discovered))
	for _, p := range discovered {
		if p.Path == "" || !artifact.SupportsPath(p.Path) {
			continue
		}
		path := filepath.Clean(p.Path)
		position, seen := positions[path]
		if !seen {
			positions[path] = len(unique)
			unique = append(unique, p)
			continue
		}
		if _, current := served[unique[position].ID.Ecosystem]; current {
			continue
		}
		if _, replacement := served[p.ID.Ecosystem]; replacement {
			unique[position] = p
		}
	}
	return unique
}

// packageMapperProviders are the hash mappers that identify installed
// packages, in trial order. A file that Modrinth identifies is managed under
// Modrinth even when CurseForge also matches it.
var packageMapperProviders = []catalog.Provider{
	catalog.ProviderModrinth,
	catalog.ProviderCurseForge,
}

// packageProbeLimit bounds concurrent provider lookups. Every probe asks each
// mapper for a release, so an unbounded fan-out over a large modpack would
// rate-limit the requests and leave identifiable packages unmanaged.
const packageProbeLimit = 8

// packageMappers returns the configured release mappers in trial order. A
// provider without a configured mapper is skipped, not an error.
func (s *Service) packageMappers() ([]packageMapper, error) {
	var mappers []packageMapper
	for _, provider := range packageMapperProviders {
		release, ok, err := s.source.GetArtifactReleaseMapper(provider)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		mappers = append(mappers, packageMapper{
			provider: provider,
			release:  release,
		})
	}
	return mappers, nil
}

func (a *packageAdopter) probe(
	ctx context.Context,
	p workspace.DiscoveredPackage,
) adoptionProposal {
	proposal := adoptionProposal{pkg: p}
	if p.ID.Ecosystem == catalog.EcosystemMCDR {
		a.probeDescriptor(ctx, &proposal, catalog.EcosystemMCDR)
		return proposal
	}
	// A provider that identifies the file already answers its project,
	// version, and release, so descriptor inspection cannot add ownership.
	if a.probeMapped(ctx, &proposal) {
		return proposal
	}
	a.probeDescriptor(ctx, &proposal, p.ID.Ecosystem)
	return proposal
}

// probeDescriptor classifies a file no provider identified. Adoption needs a
// release, so this path only explains why the file stays unmanaged.
func (a *packageAdopter) probeDescriptor(
	ctx context.Context,
	proposal *adoptionProposal,
	ecosystem catalog.Ecosystem,
) {
	p := proposal.pkg
	if ecosystem != catalog.EcosystemMCDR {
		if _, ok := a.served[ecosystem]; !ok {
			var adopted bool
			ecosystem, adopted = servedEcosystemForFile(
				ctx,
				p.Path,
				a.served,
				string(a.manifest.Server.Minecraft),
			)
			if !adopted {
				proposal.warnings = append(proposal.warnings, fmt.Errorf(
					"package %s left unmanaged: the server does not load %s",
					p.Path,
					p.ID.Ecosystem,
				))
				return
			}
		}
	}
	proposal.ecosystem = ecosystem
	modules, err := artifact.ReadModulesFor(
		ctx,
		p.Path,
		ecosystem,
		string(a.manifest.Server.Minecraft),
	)
	if err != nil {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"package %s left unmanaged: read descriptor: %w",
			p.Path,
			err,
		))
		return
	}
	if len(modules) == 0 {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"package %s left unmanaged: its descriptor declares no server-side module",
			p.Path,
		))
		return
	}
	if ecosystem == catalog.EcosystemMCDR {
		a.probeMCDR(ctx, proposal, modules[0].ID, ecosystem)
		return
	}
	proposal.warnings = append(proposal.warnings, fmt.Errorf(
		"package %s left unmanaged: no configured provider identifies its hash",
		p.Path,
	))
}

func (a *packageAdopter) probeMCDR(
	ctx context.Context,
	proposal *adoptionProposal,
	moduleID string,
	ecosystem catalog.Ecosystem,
) {
	p := proposal.pkg
	if a.manifest.MCDR == nil {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"plugin %s left unmanaged: MCDReforged is not configured",
			p.Path,
		))
		return
	}
	pluginDirectory := filepath.Join(
		a.root,
		a.manifest.MCDR.Directory,
		"plugins",
	)
	if filepath.Clean(filepath.Dir(p.Path)) != filepath.Clean(pluginDirectory) {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"plugin %s left unmanaged: MCDReforged loads it outside the default plugins directory",
			filepath.Base(p.Path),
		))
		return
	}
	candidates, err := a.service.source.Candidates(
		ctx,
		catalog.RegistryMCDR,
		moduleID,
		string(a.manifest.Server.Minecraft),
		ecosystem,
	)
	if err != nil {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"plugin %s left unmanaged: %w",
			p.Path,
			err,
		))
		return
	}
	for _, candidate := range candidates {
		if candidate.Hashes["sha256"] == "" {
			continue
		}
		if _, err := install.DigestFile(p.Path, candidate.Hashes); err != nil {
			continue
		}
		info, err := os.Lstat(p.Path)
		if err != nil {
			proposal.fatal = fmt.Errorf("inspect package %s: %w", p.Path, err)
			return
		}
		packageID := "mcdr:" + candidate.ProjectID
		proposal.key = packageKey(catalog.EcosystemMCDR, packageID)
		proposal.packageID = packageID
		proposal.reference = packageID
		proposal.sha256 = candidate.Hashes["sha256"]
		proposal.mode = uint32(info.Mode().Perm())
		proposal.candidate = candidate
		proposal.matched = true
		return
	}
	proposal.warnings = append(proposal.warnings, fmt.Errorf(
		"plugin %s left unmanaged: no listed release matches its bytes",
		p.Path,
	))
}

// probeMapped identifies an installed package by content hash. Mappers are
// tried in order: the first one that identifies the file records the package
// under its provider. The release names the ecosystem that serves it; the
// installed bytes are evidence that the server holds the artifact, so a
// release whose compatibility labels miss the target stays installable.
func (a *packageAdopter) probeMapped(
	ctx context.Context,
	proposal *adoptionProposal,
) bool {
	p := proposal.pkg
	if len(a.mappers) == 0 {
		return false
	}
	digests, err := install.DigestFile(p.Path, nil)
	if err != nil {
		proposal.fatal = fmt.Errorf("digest package %s: %w", p.Path, err)
		return false
	}
	info, err := os.Lstat(p.Path)
	if err != nil {
		proposal.fatal = fmt.Errorf("inspect package %s: %w", p.Path, err)
		return false
	}
	for _, mapper := range a.mappers {
		release, matched, err := mapper.release.ReleaseByHash(
			ctx,
			artifact.File{Path: p.Path},
		)
		if err != nil {
			// Another provider may still identify the file. Report the file
			// only when every provider failed to identify it.
			a.service.logger.Debug(
				"artifact hash lookup failed",
				"path", p.Path,
				"provider", mapper.provider,
				"error", err,
			)
			continue
		}
		if !matched || release.Ref.Name == "" {
			continue
		}
		ecosystem, ok := a.releaseEcosystem(p, release)
		if !ok {
			proposal.warnings = append(proposal.warnings, fmt.Errorf(
				"package %s matched %s but its release lists no loader this server runs",
				p.Path,
				mapper.provider,
			))
			return true
		}
		if release.ServerSide == upstream.ServerSideUnsupported {
			proposal.warnings = append(proposal.warnings, fmt.Errorf(
				"package %s left unmanaged: its release does not support a dedicated server",
				p.Path,
			))
			return true
		}
		if release.ServerSide == "" {
			if clientOnly, known := a.clientOnlyFile(ctx, p.Path, ecosystem); known &&
				clientOnly {
				proposal.warnings = append(proposal.warnings, fmt.Errorf(
					"package %s left unmanaged: its descriptor declares no server-side module",
					p.Path,
				))
				return true
			}
		}
		if release.Candidate.ProjectID == "" {
			proposal.warnings = append(proposal.warnings, fmt.Errorf(
				"package %s matched %s but its release has no project ID",
				p.Path,
				mapper.provider,
			))
			return true
		}
		if release.Candidate.URL == "" {
			// Resolution materializes the adopted release, so a release the
			// provider cannot serve would abort the whole installation.
			proposal.warnings = append(proposal.warnings, fmt.Errorf(
				"package %s matched %s but its release has no download URL",
				p.Path,
				mapper.provider,
			))
			return true
		}
		candidate := release.Candidate
		if !release.Supports(
			string(a.manifest.Server.Minecraft),
			ecosystem,
		) {
			proposal.warnings = append(proposal.warnings, fmt.Errorf(
				"package %s: %s lists release %s for another server; adopting the installed file",
				p.Path,
				mapper.provider,
				release.Ref.Version,
			))
			candidate.CompatibilityUnknown = true
		}
		provider := string(mapper.provider)
		proposal.ecosystem = ecosystem
		proposal.reference = provider + ":" + release.Ref.Name.String()
		proposal.packageID = provider + ":" + candidate.ProjectID
		proposal.key = packageKey(ecosystem, proposal.packageID)
		proposal.sha256 = digests["sha256"]
		proposal.mode = uint32(info.Mode().Perm())
		proposal.candidate = candidate
		proposal.matched = true
		return true
	}
	return false
}

// clientOnlyFile reports whether the file's descriptor for ecosystem keeps
// every module it declares off a dedicated server. known is false when no
// descriptor for ecosystem declares a module (a library or locator archive):
// the hash identifies it, and resolution reads its nested modules.
func (a *packageAdopter) clientOnlyFile(
	ctx context.Context,
	path string,
	ecosystem catalog.Ecosystem,
) (clientOnly, known bool) {
	clientOnly, known, err := artifact.ReadClientOnly(
		ctx,
		path,
		ecosystem,
		string(a.manifest.Server.Minecraft),
	)
	if err != nil {
		return false, false
	}
	return clientOnly, known
}

// releaseEcosystem selects the served ecosystem a release belongs to. The
// file's descriptor ecosystem wins when the server serves it; otherwise the
// release's loader labels decide. A release with no recognized loader labels
// belongs to any served ecosystem.
func (a *packageAdopter) releaseEcosystem(
	p workspace.DiscoveredPackage,
	release upstream.Release,
) (catalog.Ecosystem, bool) {
	if _, served := a.served[p.ID.Ecosystem]; served &&
		release.SupportsLoader(p.ID.Ecosystem) {
		return p.ID.Ecosystem, true
	}
	for _, ecosystem := range slices.Sorted(maps.Keys(a.served)) {
		if ecosystem == catalog.EcosystemMCDR {
			continue
		}
		if release.SupportsLoader(ecosystem) {
			return ecosystem, true
		}
	}
	return catalog.EcosystemUnspecified, false
}

func (a *packageAdopter) apply(proposal adoptionProposal) {
	for _, warning := range proposal.warnings {
		a.warn("%v", warning)
	}
	if !proposal.matched {
		return
	}
	if _, claimed := a.claims[proposal.pkg.Path]; claimed {
		return
	}
	if a.conflicts[proposal.key] {
		a.warn(
			"package %s left unmanaged: several installed files identify the same project",
			proposal.pkg.Path,
		)
		return
	}
	if _, exists := a.index[proposal.key]; exists {
		prior := a.markConflict(proposal.key)
		a.warn(
			"package %s left unmanaged: installed jars %s and %s identify the same project",
			proposal.reference, prior, proposal.pkg.Path,
		)
		return
	}
	requirement := documentpkg.Requirement{
		Selector: proposal.candidate.Version,
	}
	if proposal.ecosystem == catalog.EcosystemMCDR {
		a.manifest.MCDR.Packages[proposal.reference] = requirement
	} else {
		a.requirePackageMap(proposal.ecosystem)[proposal.reference] = requirement
	}
	a.record(proposal)
}

// markConflict invalidates a project key, removes its earlier claim, and
// returns the path that claim had recorded.
func (a *packageAdopter) markConflict(key string) string {
	a.conflicts[key] = true
	index, exists := a.index[key]
	if !exists {
		return ""
	}
	delete(a.index, key)
	if index >= len(a.packages) {
		return ""
	}
	prior := a.packages[index].Path
	delete(a.claims, prior)
	a.packages[index] = install.AdoptedPackage{}
	release := a.releases[index]
	a.releases[index] = install.AdoptedRelease{}
	if release.Ecosystem == catalog.EcosystemMCDR {
		delete(a.manifest.MCDR.Packages, release.Reference)
	} else {
		delete(a.manifest.Server.Packages[release.Ecosystem], release.Reference)
	}
	return prior
}

func (a *packageAdopter) record(proposal adoptionProposal) {
	a.index[proposal.key] = len(a.packages)
	a.claims[proposal.pkg.Path] = proposal.key
	a.packages = append(a.packages, install.AdoptedPackage{
		Path:           proposal.pkg.Path,
		PackageID:      proposal.packageID,
		ExpectedSHA256: proposal.sha256,
		ExpectedMode:   proposal.mode,
	})
	a.releases = append(a.releases, install.AdoptedRelease{
		Reference: proposal.reference,
		Ecosystem: proposal.ecosystem,
		Candidate: proposal.candidate,
	})
}

func (a *packageAdopter) requirePackageMap(
	ecosystem catalog.Ecosystem,
) map[string]documentpkg.Requirement {
	if a.manifest.Server.Packages[ecosystem] == nil {
		a.manifest.Server.Packages[ecosystem] = map[string]documentpkg.Requirement{}
	}
	return a.manifest.Server.Packages[ecosystem]
}

func packageKey(ecosystem catalog.Ecosystem, packageID string) string {
	return string(ecosystem) + "\x00" + packageID
}

func (a *packageAdopter) warn(format string, args ...any) {
	if a.observed == nil {
		return
	}
	a.observed.Warnings = append(a.observed.Warnings, fmt.Errorf(format, args...))
}
