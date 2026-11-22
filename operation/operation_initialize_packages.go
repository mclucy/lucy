package operation

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
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
	mappers  []upstream.ArtifactReleaseMapper

	accepted  []acceptedPackage
	index     map[string]int
	conflicts map[string]bool

	target       resolve.Target
	moduleClaims map[resolve.ModuleIdentity]moduleClaim
}

// acceptedPackage is one installed file adoption manages. The embedded
// release is the resolver input; PackageID and Mode complete the receipt
// entry. A conflict invalidates this record so the file contributes neither
// a receipt entry nor a resolver candidate. The record keeps its index while
// invalid, so the module claims of accepted files stay addressable.
type acceptedPackage struct {
	install.AdoptedRelease
	PackageID string
	Mode      uint32
	valid     bool
}

// moduleClaim names the recorded package that answers one module identity.
type moduleClaim struct {
	index  int
	module catalog.DescriptorModule
}

// moduleCollision pairs a proposed module with a claim on one of its
// identities.
type moduleCollision struct {
	identity resolve.ModuleIdentity
	module   catalog.DescriptorModule
	prior    moduleClaim
}

// adoptionProposal is one file's probe result. Probing reads the file and its
// upstream identity without mutating the adopter, so probes can run in
// parallel and apply in a deterministic order.
type adoptionProposal struct {
	pkg       workspace.DiscoveredPackage
	analysis  *artifact.Analysis
	ecosystem catalog.Ecosystem
	key       string
	reference string
	packageID string
	sha256    string
	mode      uint32
	candidate upstream.Candidate
	matched   bool
	modules   []catalog.DescriptorModule
	fatal     error
	warnings  []error
	// mapped marks a file whose local evidence is collected and which joins
	// the shared hash lookup.
	mapped bool
}

// adoptInstalledPackages adds requirements for the packages discovery
// identified and returns the ownership claims installation must record,
// together with the releases resolution selects for them and the absent
// modules the adoption report already named. known holds the resolutions an
// earlier identification pass obtained for these paths, so adoption never
// resends them to the provider chain.
func (s *Service) adoptInstalledPackages(
	ctx context.Context,
	root string,
	manifest *documentpkg.Manifest,
	observed *workspace.Workspace,
	known map[string]upstream.ArtifactReleaseResolution,
) (
	packages []install.AdoptedPackage,
	releases []install.AdoptedRelease,
	reportedMissing map[string]struct{},
	inspected int,
	err error,
) {
	mappers, err := s.packageMappers()
	if err != nil {
		return nil, nil, nil, 0, err
	}
	mcdrVersion := ""
	if manifest.MCDR != nil {
		mcdrVersion = string(manifest.MCDR.Version)
	}
	adopter := &packageAdopter{
		service:   s,
		root:      root,
		manifest:  manifest,
		observed:  observed,
		served:    servedEcosystemSet(manifest, observed),
		mappers:   mappers,
		index:     map[string]int{},
		conflicts: map[string]bool{},
		target: resolve.Target{
			Product:     manifest.Server.Core.Product,
			Minecraft:   manifest.Server.Minecraft,
			CoreVersion: manifest.Server.Core.ProductVersion,
			MCDRVersion: mcdrVersion,
		},
		moduleClaims: map[resolve.ModuleIdentity]moduleClaim{},
	}
	var discovered []workspace.DiscoveredPackage
	if observed != nil {
		discovered = observed.Packages
	}
	unique := uniquePackages(discovered, adopter.served)
	proposals := make([]adoptionProposal, len(unique))
	parallelPackageProbes(ctx, len(unique), func(i int) {
		proposals[i] = adopter.prepare(unique[i])
	})
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, 0, err
	}
	// Unreadable local evidence invalidates the whole adoption, so this
	// check precedes every provider lookup.
	for _, proposal := range proposals {
		if proposal.fatal != nil {
			return nil, nil, nil, 0, proposal.fatal
		}
	}
	resolutions := adopter.lookupReleases(ctx, proposals, known)
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, 0, err
	}
	parallelPackageProbes(ctx, len(proposals), func(i int) {
		adopter.complete(ctx, &proposals[i], resolutions[i])
	})
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, 0, err
	}
	for i := range proposals {
		if err := proposals[i].fatal; err != nil {
			return nil, nil, nil, 0, err
		}
	}
	for _, proposal := range proposals {
		adopter.apply(proposal)
	}
	if observed != nil {
		for _, path := range observed.UnidentifiedPackageFiles {
			if lookupErrors := known[path].Errors; len(lookupErrors) != 0 {
				adopter.warn("%v", incompleteHashLookupWarning("package", path, lookupErrors))
			}
		}
	}
	reportedMissing = adopter.reportMissingModules(proposals)
	// Derive the receipt entries and the resolver input from the accepted
	// records in one pass; an invalidated record contributes to neither.
	packages = make([]install.AdoptedPackage, 0, len(adopter.accepted))
	releases = make([]install.AdoptedRelease, 0, len(adopter.accepted))
	for _, accepted := range adopter.accepted {
		if !accepted.valid {
			continue
		}
		packages = append(packages, install.AdoptedPackage{
			Path:           accepted.Path,
			PackageID:      accepted.PackageID,
			ExpectedSHA256: accepted.SHA256,
			ExpectedMode:   accepted.Mode,
		})
		releases = append(releases, accepted.AdoptedRelease)
	}
	s.logger.Debug(
		"recognized existing packages",
		"count", len(packages),
		"present", len(discovered),
	)
	return packages, releases, reportedMissing, len(unique), nil
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

// packageProbeLimit bounds concurrent package I/O during initialization and
// import. Probes read local descriptors and collect digests; one shared lookup
// then batches provider requests, so an unbounded fan-out over a large modpack
// cannot rate-limit them.
const packageProbeLimit = 8

func parallelPackageProbes(ctx context.Context, count int, run func(int)) {
	var next atomic.Int64
	var wg sync.WaitGroup
	for range min(count, packageProbeLimit) {
		wg.Go(func() {
			for ctx.Err() == nil {
				i := int(next.Add(1) - 1)
				if i >= count {
					return
				}
				run(i)
			}
		})
	}
	wg.Wait()
}

// packageMappers returns the configured release mappers in trial order. A
// provider without a configured mapper is skipped, not an error.
func (s *Service) packageMappers() ([]upstream.ArtifactReleaseMapper, error) {
	var mappers []upstream.ArtifactReleaseMapper
	for _, provider := range packageMapperProviders {
		release, ok, err := s.source.GetArtifactReleaseMapper(provider)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		mappers = append(mappers, release)
	}
	return mappers, nil
}

// prepare collects the local evidence a hash lookup needs. A file whose digest
// or metadata cannot be read fails adoption before any provider is queried, and
// a file MCDR owns or no mapper covers is never sent to the mappers.
func (a *packageAdopter) prepare(
	p workspace.DiscoveredPackage,
) adoptionProposal {
	proposal := adoptionProposal{pkg: p, analysis: a.observed.PackageAnalysis(p.Path)}
	if p.ID.Ecosystem == catalog.EcosystemMCDR || len(a.mappers) == 0 {
		return proposal
	}
	sha256, err := proposal.analysis.SHA256()
	if err != nil {
		proposal.fatal = fmt.Errorf("digest package %s: %w", p.Path, err)
		return proposal
	}
	info, err := os.Lstat(p.Path)
	if err != nil {
		proposal.fatal = fmt.Errorf("inspect package %s: %w", p.Path, err)
		return proposal
	}
	proposal.mapped = true
	proposal.sha256 = sha256
	proposal.mode = uint32(info.Mode().Perm())
	return proposal
}

// lookupReleases resolves the prepared files identification has not already
// resolved. A path an earlier pass resolved, including a clean miss or a
// provider failure, is reused as is, so each file enters the provider chain at
// most once. The remaining files share one batched lookup. A provider
// identifies a package only by a named release, so a mapper that matched
// without a project name leaves the file to the next provider.
func (a *packageAdopter) lookupReleases(
	ctx context.Context,
	proposals []adoptionProposal,
	known map[string]upstream.ArtifactReleaseResolution,
) []upstream.ArtifactReleaseResolution {
	resolutions := make([]upstream.ArtifactReleaseResolution, len(proposals))
	fingerprints := make([]upstream.ArtifactFingerprint, 0, len(proposals))
	positions := make([]int, 0, len(proposals))
	for i := range proposals {
		if !proposals[i].mapped {
			// MCDR uses its own catalogue to match releases, but can retain
			// diagnostics from an earlier lookup without querying mappers.
			resolutions[i].Errors = known[proposals[i].pkg.Path].Errors
			continue
		}
		if resolution, ok := known[proposals[i].pkg.Path]; ok {
			resolutions[i] = resolution
			continue
		}
		positions = append(positions, i)
		fingerprints = append(fingerprints, proposals[i].analysis)
	}
	if len(fingerprints) == 0 {
		return resolutions
	}
	found := upstream.LookupArtifactReleases(
		ctx,
		fingerprints,
		a.mappers,
		func(release upstream.Release) bool { return release.Ref.Name != "" },
	)
	for j, position := range positions {
		resolutions[position] = found[j]
	}
	return resolutions
}

// complete turns one resolution into either an adopted release or the
// descriptor report that explains why the file stays unmanaged. A provider
// failure never excludes the file: another provider may still identify it, and
// otherwise the file is reported once.
func (a *packageAdopter) complete(
	ctx context.Context,
	proposal *adoptionProposal,
	resolution upstream.ArtifactReleaseResolution,
) {
	for _, providerErr := range resolution.Errors {
		a.service.logger.Debug(
			"artifact hash lookup failed",
			"path", proposal.pkg.Path,
			"provider", providerErr.Provider,
			"error", providerErr.Err,
		)
	}
	if resolution.Matched {
		// A provider that identifies the file already answers its project,
		// version, and release, so descriptor inspection cannot add ownership.
		a.adoptMapped(ctx, proposal, resolution)
		return
	}
	a.probeDescriptor(ctx, proposal, resolution.Errors)
}

// probeDescriptor classifies a file no provider identified. Adoption needs a
// release, so this path only explains why the file stays unmanaged.
func (a *packageAdopter) probeDescriptor(
	ctx context.Context,
	proposal *adoptionProposal,
	lookupErrors []upstream.ProviderError,
) {
	p := proposal.pkg
	ecosystem := p.ID.Ecosystem
	if ecosystem != catalog.EcosystemMCDR {
		if _, ok := a.served[ecosystem]; !ok {
			var adopted bool
			ecosystem, adopted = servedEcosystemForFile(
				ctx,
				proposal.analysis,
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
	view, err := proposal.analysis.Descriptor(ctx, ecosystem, string(a.manifest.Server.Minecraft))
	if err != nil {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"package %s left unmanaged: read descriptor: %w",
			p.Path,
			err,
		))
		return
	}
	if view.ClientOnly {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"package %s left unmanaged: client-only %s",
			p.Path,
			ecosystem,
		))
		return
	}
	modules := view.Modules
	if len(modules) == 0 {
		proposal.warnings = append(proposal.warnings, a.importWarning(
			importWarningNoServerModule,
			p.Path,
			"package %s left unmanaged: its descriptor declares no server-side module",
			p.Path,
		))
		return
	}
	proposal.modules = modules
	if ecosystem == catalog.EcosystemMCDR {
		a.probeMCDR(ctx, proposal, lookupErrors)
		return
	}
	if len(lookupErrors) != 0 {
		proposal.warnings = append(proposal.warnings, incompleteHashLookupWarning("package", p.Path, lookupErrors))
		return
	}
	proposal.warnings = append(proposal.warnings, a.importWarning(
		importWarningNoProvider,
		p.Path,
		"package %s left unmanaged: no configured provider identifies its hash",
		p.Path,
	))
}

func (a *packageAdopter) probeMCDR(
	ctx context.Context,
	proposal *adoptionProposal,
	lookupErrors []upstream.ProviderError,
) {
	p := proposal.pkg
	moduleID := proposal.modules[0].ID
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
		catalog.EcosystemMCDR,
	)
	if err != nil {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"plugin %s left unmanaged: MCDReforged catalogue lookup failed: %w",
			p.Path,
			err,
		))
		return
	}
	sha256, err := proposal.analysis.SHA256()
	if err != nil {
		proposal.fatal = fmt.Errorf("digest package %s: %w", p.Path, err)
		return
	}
	for _, candidate := range candidates {
		if !strings.EqualFold(candidate.Hashes["sha256"], sha256) {
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
		proposal.sha256 = sha256
		proposal.mode = uint32(info.Mode().Perm())
		proposal.candidate = candidate
		proposal.matched = true
		return
	}
	if len(lookupErrors) != 0 {
		proposal.warnings = append(proposal.warnings, incompleteHashLookupWarning("plugin", p.Path, lookupErrors))
		return
	}
	proposal.warnings = append(proposal.warnings, fmt.Errorf(
		"plugin %s left unmanaged: no listed release matches its bytes",
		p.Path,
	))
}

func incompleteHashLookupWarning(kind, path string, failures []upstream.ProviderError) error {
	reasons := make([]string, len(failures))
	for i, failure := range failures {
		reasons[i] = failure.Error()
	}
	return fmt.Errorf("%s %s left unmanaged: provider hash lookup incomplete: %s", kind, path, strings.Join(reasons, "; "))
}

// adoptMapped records an installed package a provider identified by content
// hash. The file's own descriptor decides whether a dedicated server can load
// it; the release only supplies identity, a download URL, and advisory labels,
// so a remote side label never excludes an installed file the descriptor
// accepts. A loader mismatch is reported but does not by itself prevent
// adoption.
func (a *packageAdopter) adoptMapped(
	ctx context.Context,
	proposal *adoptionProposal,
	resolution upstream.ArtifactReleaseResolution,
) {
	p := proposal.pkg
	release := resolution.Release
	provider := resolution.Provider
	ecosystem, local := a.descriptorEcosystem(ctx, p, proposal.analysis)
	if !local {
		served, ok := a.releaseEcosystem(p, release)
		if !ok {
			a.warnLoaderMismatch(
				proposal,
				provider,
				release,
				a.serverEcosystem(),
			)
			return
		}
		ecosystem = served
	}
	clientOnly, known, sideEcosystem, sideErr := a.descriptorSide(
		ctx,
		proposal.analysis,
		ecosystem,
		p.ID.Ecosystem,
	)
	loaderMismatch := !release.SupportsLoader(ecosystem)
	if sideErr != nil {
		if loaderMismatch {
			a.warnLoaderMismatch(proposal, provider, release, ecosystem)
		} else {
			proposal.warnings = append(proposal.warnings, fmt.Errorf(
				"package %s left unmanaged: read descriptor: %w",
				p.Path,
				sideErr,
			))
		}
		return
	}
	if known && clientOnly {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"package %s left unmanaged: client-only %s",
			p.Path,
			sideEcosystem,
		))
		return
	}
	if loaderMismatch {
		a.warnLoaderMismatch(proposal, provider, release, ecosystem)
		if !known {
			return
		}
	}
	// A mapped release becomes a locked requirement, so its descriptor
	// must be readable and declare a server-visible module. A library
	// archive is loaded by the loader but is not a package Lucy can
	// resolve, so it stays unmanaged.
	view, readErr := proposal.analysis.Descriptor(ctx, ecosystem, string(a.manifest.Server.Minecraft))
	if readErr != nil {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"package %s left unmanaged: read descriptor: %w",
			p.Path,
			readErr,
		))
		return
	}
	modules := view.Modules
	if len(modules) == 0 {
		// A descriptor without a server module is not evidence the server
		// can load the file; resolution cannot satisfy the locked build.
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"package %s left unmanaged: its descriptor declares no server-side module",
			p.Path,
		))
		return
	}
	proposal.modules = modules
	if !known {
		switch {
		case release.ServerSide == upstream.ServerSideUnsupported:
			proposal.warnings = append(proposal.warnings, a.importWarning(
				importWarningRemoteSide,
				p.Path,
				"package %s left unmanaged: its release does not support a dedicated server",
				p.Path,
			))
			return
		case release.ClientOnly():
			proposal.warnings = append(proposal.warnings, a.importWarning(
				importWarningRemoteSide,
				p.Path,
				"package %s left unmanaged: its release declares client-only support",
				p.Path,
			))
			return
		}
	}
	if release.Candidate.ProjectID == "" {
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"package %s matched %s but its release has no project ID",
			p.Path,
			provider,
		))
		return
	}
	if release.Candidate.URL == "" {
		// A lock needs a source for replay when its store object is missing.
		proposal.warnings = append(proposal.warnings, fmt.Errorf(
			"package %s matched %s but its release has no download URL",
			p.Path,
			provider,
		))
		return
	}
	candidate := release.Candidate
	if !release.Supports(
		string(a.manifest.Server.Minecraft),
		ecosystem,
	) {
		if !loaderMismatch {
			proposal.warnings = append(proposal.warnings, fmt.Errorf(
				"package %s: %s lists release %s for another server; adopting the installed file",
				p.Path,
				provider,
				release.Ref.Version,
			))
		}
		candidate.CompatibilityUnknown = true
	}
	providerName := string(provider)
	proposal.ecosystem = ecosystem
	proposal.reference = providerName + ":" + release.Ref.Name.String()
	proposal.packageID = providerName + ":" + candidate.ProjectID
	proposal.key = packageKey(ecosystem, proposal.packageID)
	proposal.candidate = candidate
	proposal.matched = true
}

// descriptorSide reads the file's side evidence. The served loader decides
// first; a file it has no descriptor for (a Fabric jar bridged by Connector)
// is checked with the descriptor the file carries, so a client-only bridge
// cannot slip onto the server.
func (a *packageAdopter) descriptorSide(
	ctx context.Context,
	analysis *artifact.Analysis,
	ecosystem catalog.Ecosystem,
	fileEcosystem catalog.Ecosystem,
) (clientOnly, known bool, sideEcosystem catalog.Ecosystem, err error) {
	minecraft := string(a.manifest.Server.Minecraft)
	view, err := analysis.Descriptor(ctx, ecosystem, minecraft)
	clientOnly, known = view.ClientOnly, view.Known
	if err != nil || known || fileEcosystem == ecosystem ||
		fileEcosystem == catalog.EcosystemUnspecified ||
		fileEcosystem == catalog.EcosystemMCDR {
		return clientOnly, known, ecosystem, err
	}
	if _, served := a.served[fileEcosystem]; !served {
		return clientOnly, known, ecosystem, err
	}
	bridged, bridgedErr := analysis.Descriptor(ctx, fileEcosystem, minecraft)
	if bridgedErr == nil && bridged.Known {
		return bridged.ClientOnly, true, fileEcosystem, nil
	}
	return clientOnly, known, ecosystem, err
}

// descriptorEcosystem reports the served loader whose descriptor the file
// carries. The file's own descriptor ecosystem wins; a file the server has no
// descriptor for (a bridged Fabric jar) is tried with each served loader.
func (a *packageAdopter) descriptorEcosystem(
	ctx context.Context,
	p workspace.DiscoveredPackage,
	analysis *artifact.Analysis,
) (catalog.Ecosystem, bool) {
	if _, served := a.served[p.ID.Ecosystem]; served &&
		p.ID.Ecosystem != catalog.EcosystemMCDR {
		return p.ID.Ecosystem, true
	}
	return servedEcosystemForFile(
		ctx,
		analysis,
		a.served,
		string(a.manifest.Server.Minecraft),
	)
}

// serverEcosystem names the loader a warning should attribute the server to.
func (a *packageAdopter) serverEcosystem() catalog.Ecosystem {
	product := catalog.Ecosystem(a.manifest.Server.Core.Product)
	if product != catalog.EcosystemUnspecified &&
		product != catalog.EcosystemMinecraft {
		return product
	}
	for _, ecosystem := range slices.Sorted(maps.Keys(a.served)) {
		if ecosystem != catalog.EcosystemMCDR {
			return ecosystem
		}
	}
	if product != catalog.EcosystemUnspecified {
		return product
	}
	return catalog.EcosystemMinecraft
}

// warnLoaderMismatch reports a release whose loader labels do not include the
// loader the server runs. The message is reporting only: a local descriptor
// that proves the file loads still lets adoption manage it.
func (a *packageAdopter) warnLoaderMismatch(
	proposal *adoptionProposal,
	provider catalog.Provider,
	release upstream.Release,
	ecosystem catalog.Ecosystem,
) {
	proposal.warnings = append(proposal.warnings, a.importWarning(
		importWarningLoaderMismatch,
		proposal.pkg.Path,
		"package %s matched %s but its release targets %s while the server runs %s",
		proposal.pkg.Path,
		provider,
		strings.Join(release.Loaders, ", "),
		ecosystem,
	))
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
		var importWarn *importWarning
		if errors.As(warning, &importWarn) {
			if a.observed != nil {
				a.observed.Warnings = append(a.observed.Warnings, importWarn)
			}
			continue
		}
		a.warn("%v", warning)
	}
	if !proposal.matched {
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
	if len(proposal.modules) > 0 && a.resolveModuleCollisions(proposal) {
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

// proposalEnvironment scopes a proposal's module identities like the
// resolver's packages.
func proposalEnvironment(proposal adoptionProposal) serverpkg.Environment {
	if proposal.ecosystem == catalog.EcosystemMCDR {
		return serverpkg.EnvironmentMCDR
	}
	return serverpkg.EnvironmentServer
}

// nativeModule reports whether the artifact itself declares the module. A
// bundled module is a loader-selected library; several packages may ship the
// same one, and resolution picks the version. It is not a file conflict.
func nativeModule(module catalog.DescriptorModule) bool {
	return module.NestedPath == ""
}

// moduleCollisions lists the proposal's module identities another recorded
// file already provides.
func (a *packageAdopter) moduleCollisions(
	proposal adoptionProposal,
) []moduleCollision {
	var collisions []moduleCollision
	for _, module := range proposal.modules {
		if !nativeModule(module) {
			continue
		}
		for _, identity := range a.target.ModuleIdentityKeys(
			proposalEnvironment(proposal),
			module.Ecosystem,
			module,
		) {
			prior, claimed := a.moduleClaims[identity]
			if !claimed {
				continue
			}
			collisions = append(collisions, moduleCollision{
				identity: identity,
				module:   module,
				prior:    prior,
			})
		}
	}
	return collisions
}

// resolveModuleCollisions keeps one file for each module identity. It reports
// whether the proposal loses an identity and must stay unmanaged.
func (a *packageAdopter) resolveModuleCollisions(
	proposal adoptionProposal,
) bool {
	collisions := a.moduleCollisions(proposal)
	if len(collisions) == 0 {
		return false
	}
	for _, collision := range collisions {
		if resolve.ModuleOrder(collision.prior.module, collision.module) >= 0 {
			a.warnModuleCollision(
				proposal.pkg.Path,
				a.accepted[collision.prior.index].Path,
				collision.identity,
				proposal.ecosystem,
			)
			return true
		}
	}
	var displaced []moduleCollision
	for _, collision := range collisions {
		if slices.ContainsFunc(displaced, func(prior moduleCollision) bool {
			return prior.prior.index == collision.prior.index
		}) {
			continue
		}
		displaced = append(displaced, collision)
	}
	paths := make([]string, len(displaced))
	for i, collision := range displaced {
		paths[i] = a.accepted[collision.prior.index].Path
	}
	for _, collision := range displaced {
		a.unclaim(collision.prior.index)
	}
	for i, collision := range displaced {
		a.warnModuleCollision(
			paths[i],
			proposal.pkg.Path,
			collision.identity,
			proposal.ecosystem,
		)
	}
	return false
}

// warnModuleCollision reports one file left unmanaged because another file
// provides the same module identity.
func (a *packageAdopter) warnModuleCollision(
	loserPath string,
	winnerPath string,
	identity resolve.ModuleIdentity,
	ecosystem catalog.Ecosystem,
) {
	impact := fmt.Sprintf("%s rejects duplicate module IDs", ecosystem)
	if ecosystem == catalog.EcosystemMCDR {
		impact = "MCDReforged loads both plugins under the same ID"
	}
	a.warn(
		"package %s left unmanaged: it declares %s module %q, which %s already provides; %s. To keep it instead, remove %s and run `lucy add <file>`.",
		loserPath,
		ecosystem,
		identity.ID,
		winnerPath,
		impact,
		winnerPath,
	)
}

// unclaim invalidates a recorded package and removes its project index,
// module claims, and requirement.
func (a *packageAdopter) unclaim(index int) {
	if index < 0 || index >= len(a.accepted) {
		return
	}
	accepted := &a.accepted[index]
	if !accepted.valid {
		return
	}
	accepted.valid = false
	delete(a.index, packageKey(accepted.Ecosystem, accepted.PackageID))
	if accepted.Ecosystem == catalog.EcosystemMCDR {
		delete(a.manifest.MCDR.Packages, accepted.Reference)
	} else {
		delete(a.manifest.Server.Packages[accepted.Ecosystem], accepted.Reference)
	}
	a.dropModuleClaims(index)
}

// dropModuleClaims deletes the module claims recorded for one package.
func (a *packageAdopter) dropModuleClaims(index int) {
	for identity, claim := range a.moduleClaims {
		if claim.index == index {
			delete(a.moduleClaims, identity)
		}
	}
}

// markConflict invalidates a project key, removes its earlier claim, and
// returns the path that claim had recorded.
func (a *packageAdopter) markConflict(key string) string {
	a.conflicts[key] = true
	index, exists := a.index[key]
	if !exists {
		return ""
	}
	prior := a.accepted[index].Path
	a.unclaim(index)
	return prior
}

func (a *packageAdopter) record(proposal adoptionProposal) {
	index := len(a.accepted)
	a.index[proposal.key] = index
	a.accepted = append(a.accepted, acceptedPackage{
		AdoptedRelease: install.AdoptedRelease{
			Reference: proposal.reference,
			Ecosystem: proposal.ecosystem,
			Candidate: proposal.candidate,
			Path:      proposal.pkg.Path,
			SHA256:    proposal.sha256,
			Analysis:  proposal.analysis,
		},
		PackageID: proposal.packageID,
		Mode:      proposal.mode,
		valid:     true,
	})
	for _, module := range proposal.modules {
		if !nativeModule(module) {
			continue
		}
		for _, identity := range a.target.ModuleIdentityKeys(
			proposalEnvironment(proposal),
			module.Ecosystem,
			module,
		) {
			a.moduleClaims[identity] = moduleClaim{index: index, module: module}
		}
	}
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

// reportMissingModules warns about required descriptor modules that no
// installed file provides and returns the identities it reported. It reads only
// the descriptors the probes already parsed, so it adds no provider lookups.
func (a *packageAdopter) reportMissingModules(
	proposals []adoptionProposal,
) map[string]struct{} {
	if a.observed == nil {
		return nil
	}
	provided := map[string]struct{}{}
	clientOnly := map[string]struct{}{}
	for _, proposal := range proposals {
		for _, module := range proposal.modules {
			addProvidedIdentity(provided, module)
			for _, id := range module.ClientOnly {
				clientOnly[id] = struct{}{}
			}
		}
	}
	required := map[string][]string{}
	for _, proposal := range proposals {
		path := a.displayPath(proposal.pkg.Path)
		for _, module := range proposal.modules {
			for _, dependency := range module.Dependencies {
				if dependency.Kind != catalog.DependencyKindRequired {
					continue
				}
				id := dependency.ID
				if id == "" || runtimeModuleIdentity(id) {
					continue
				}
				if _, skip := clientOnly[id]; skip {
					continue
				}
				if _, ok := provided[id]; ok {
					continue
				}
				required[id] = appendUniqueString(required[id], path)
			}
		}
	}
	reported := make(map[string]struct{}, len(required))
	for _, id := range slices.Sorted(maps.Keys(required)) {
		paths := required[id]
		reported[id] = struct{}{}
		a.warn(
			"module %s is required by %d installed files but no installed file provides it (%s)",
			id,
			len(paths),
			strings.Join(firstExamples(paths, 3), ", "),
		)
	}
	return reported
}

// addProvidedIdentity records every module identity an installed file
// provides to the server.
func addProvidedIdentity(
	provided map[string]struct{},
	module catalog.DescriptorModule,
) {
	add := func(id string) {
		if id != "" {
			provided[id] = struct{}{}
		}
	}
	add(module.ID)
	if module.Group != "" {
		add(module.Group + ":" + module.ID)
	}
	for _, id := range module.Provides {
		add(id)
		if module.Group != "" {
			add(module.Group + ":" + id)
		}
	}
}

// runtimeModuleIdentity reports a module the server loader itself provides, so
// an installed file is not expected to ship it.
func runtimeModuleIdentity(id string) bool {
	switch strings.ToLower(id) {
	case "minecraft",
		"java",
		"forge",
		"neoforge",
		"fml",
		"fabric",
		"fabricloader",
		"fabric-loader",
		"quilt",
		"quilt_loader",
		"quilt-loader",
		"bukkit",
		"paper",
		"spigot",
		"bungeecord",
		"velocity",
		"sponge",
		"mcdr":
		return true
	default:
		return false
	}
}

func appendUniqueString(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

// firstExamples keeps at most limit distinct paths for a summarized warning.
func firstExamples(paths []string, limit int) []string {
	examples := make([]string, 0, limit)
	for _, path := range paths {
		if slices.Contains(examples, path) {
			continue
		}
		if len(examples) == limit {
			break
		}
		examples = append(examples, path)
	}
	return examples
}

// displayPath reports a path relative to the workspace, so warnings never leak
// the staging directory.
func (a *packageAdopter) displayPath(path string) string {
	if path == "" {
		return ""
	}
	relative, err := filepath.Rel(a.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

// importWarning builds a warning whose message and example path are relative
// to the workspace.
func (a *packageAdopter) importWarning(
	cause importWarningCause,
	path, format string,
	args ...any,
) *importWarning {
	message := fmt.Sprintf(format, args...)
	if a.root != "" {
		message = strings.ReplaceAll(
			message,
			a.root+string(filepath.Separator),
			"",
		)
	}
	return &importWarning{
		cause:   cause,
		path:    a.displayPath(path),
		message: message,
	}
}

func (a *packageAdopter) warn(format string, args ...any) {
	if a.observed == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	message = strings.ReplaceAll(message, a.root+string(filepath.Separator), "")
	a.observed.Warnings = append(a.observed.Warnings, errors.New(message))
}
