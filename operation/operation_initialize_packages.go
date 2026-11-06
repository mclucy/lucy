package operation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/workspace"
)

// packageAdopter recognizes installed packages against the servers and
// ecosystems this workspace already loads. A manifest requirement names one
// project, so two installed jars that identify the same project cannot both be
// managed: the key would record only the last file while the receipt and
// fingerprint claim both. Conflicting projects stay unmanaged.
type packageAdopter struct {
	service   *Service
	root      string
	manifest  *documentpkg.Manifest
	observed  *workspace.Workspace
	served    map[catalog.Ecosystem]struct{}
	mapper    upstream.ArtifactMapProvider
	hasMapper bool

	recognized int
	packages   []install.AdoptedPackage
	index      map[string]int
	references map[string]string
	conflicts  map[string]bool
}

// adoptInstalledPackages adds requirements for the packages discovery
// identified and returns the ownership claims installation must record.
func (s *Service) adoptInstalledPackages(
	ctx context.Context,
	root string,
	manifest *documentpkg.Manifest,
	observed *workspace.Workspace,
) ([]install.AdoptedPackage, error) {
	mapper, hasMapper, err := s.source.GetArtifactMapper(
		catalog.ProviderModrinth,
	)
	if err != nil {
		return nil, err
	}
	adopter := &packageAdopter{
		service:    s,
		root:       root,
		manifest:   manifest,
		observed:   observed,
		served:     servedEcosystemSet(manifest, observed),
		mapper:     mapper,
		hasMapper:  hasMapper,
		index:      map[string]int{},
		references: map[string]string{},
		conflicts:  map[string]bool{},
	}
	var packages []workspace.DiscoveredPackage
	if observed != nil {
		packages = observed.Packages
	}
	for _, p := range packages {
		if err := adopter.adopt(ctx, p); err != nil {
			return nil, err
		}
	}
	adopter.packages = slices.DeleteFunc(
		adopter.packages,
		func(pkg install.AdoptedPackage) bool { return pkg.Path == "" },
	)
	s.logger.Debug(
		"recognized existing packages",
		"count", adopter.recognized,
		"present", len(packages),
	)
	return adopter.packages, nil
}

func (a *packageAdopter) adopt(
	ctx context.Context,
	p workspace.DiscoveredPackage,
) error {
	if !artifact.SupportsPath(p.Path) {
		return nil
	}
	ecosystem := p.ID.Ecosystem
	if ecosystem != catalog.EcosystemMCDR {
		if _, ok := a.served[ecosystem]; !ok {
			var adopted bool
			ecosystem, adopted = servedEcosystemForFile(ctx, p.Path, a.served)
			if !adopted {
				return nil
			}
		}
	}
	modules, err := artifact.ReadModules(ctx, p.Path, ecosystem)
	if err != nil || len(modules) == 0 {
		return nil
	}
	if ecosystem == catalog.EcosystemMCDR {
		if a.manifest.MCDR == nil {
			return nil
		}
		return a.adoptMCDR(ctx, p, modules[0].ID, ecosystem)
	}
	return a.adoptModrinth(ctx, p, ecosystem)
}

func (a *packageAdopter) adoptMCDR(
	ctx context.Context,
	p workspace.DiscoveredPackage,
	moduleID string,
	ecosystem catalog.Ecosystem,
) error {
	pluginDirectory := filepath.Join(
		a.root,
		a.manifest.MCDR.Directory,
		"plugins",
	)
	if filepath.Clean(filepath.Dir(p.Path)) != filepath.Clean(pluginDirectory) {
		a.warn(
			"plugin %s left unmanaged: MCDReforged loads it outside the default plugins directory",
			filepath.Base(p.Path),
		)
		return nil
	}
	candidates, err := a.service.source.Candidates(
		ctx,
		catalog.RegistryMCDR,
		moduleID,
		string(a.manifest.Server.Minecraft),
		ecosystem,
	)
	if err != nil {
		return nil
	}
	for _, candidate := range candidates {
		if candidate.Hashes["sha256"] == "" {
			continue
		}
		if _, err := install.DigestFile(p.Path, candidate.Hashes); err != nil {
			continue
		}
		return a.claimMCDR(p, candidate)
	}
	return nil
}

func (a *packageAdopter) claimMCDR(
	p workspace.DiscoveredPackage,
	candidate upstream.Candidate,
) error {
	info, err := os.Lstat(p.Path)
	if err != nil {
		return fmt.Errorf("inspect package %s: %w", p.Path, err)
	}
	packageID := "mcdr:" + candidate.ProjectID
	key := packageKey(catalog.EcosystemMCDR, packageID)
	if a.conflicts[key] {
		a.warn(
			"package %s left unmanaged: several installed plugins identify the same project",
			candidate.ProjectID,
		)
		return nil
	}
	if _, exists := a.index[key]; exists {
		prior := a.markConflict(key)
		delete(a.manifest.MCDR.Packages, packageID)
		a.warn(
			"package %s left unmanaged: installed plugins %s and %s identify the same project",
			candidate.ProjectID, prior, p.Path,
		)
		return nil
	}
	a.manifest.MCDR.Packages[packageID] = documentpkg.Requirement{
		Selector: candidate.Version,
	}
	a.record(key, p, packageID, candidate.Hashes["sha256"], uint32(info.Mode().Perm()))
	return nil
}

func (a *packageAdopter) adoptModrinth(
	ctx context.Context,
	p workspace.DiscoveredPackage,
	ecosystem catalog.Ecosystem,
) error {
	if !supportsModrinthPackages(ecosystem) || !a.hasMapper {
		return nil
	}
	digests, err := install.DigestFile(p.Path, nil)
	if err != nil {
		return fmt.Errorf("digest package %s: %w", p.Path, err)
	}
	info, err := os.Lstat(p.Path)
	if err != nil {
		return fmt.Errorf("inspect package %s: %w", p.Path, err)
	}
	ref, _, matched, err := a.mapper.PackageByHash(
		ctx,
		artifact.File{Path: p.Path},
	)
	if err != nil || !matched || ref.Name == "" {
		return nil
	}
	// A manifest entry that resolution cannot select blocks every later
	// command, so an installed package whose catalogue metadata offers no
	// matching release for this server stays unmanaged.
	release, err := a.modrinthRelease(ctx, ref, ecosystem)
	if err != nil {
		a.warn("package %s left unmanaged: %w", ref.Name, err)
		return nil
	}
	if release.ProjectID == "" {
		a.warn(
			"package %s left unmanaged: matched release has no project ID",
			ref.Name,
		)
		return nil
	}
	reference := "modrinth:" + ref.Name.String()
	packageID := "modrinth:" + release.ProjectID
	key := packageKey(ecosystem, packageID)
	if a.conflicts[key] {
		a.warn(
			"package %s left unmanaged: several installed jars identify the same project",
			ref.Name,
		)
		return nil
	}
	if _, exists := a.index[key]; exists {
		prior := a.markConflict(key)
		delete(a.manifest.Server.Packages[ecosystem], a.references[key])
		delete(a.references, key)
		a.warn(
			"package %s left unmanaged: installed jars %s and %s identify the same project",
			ref.Name, prior, p.Path,
		)
		return nil
	}
	a.requirePackageMap(ecosystem)[reference] = documentpkg.Requirement{
		Selector: ref.Version,
	}
	a.references[key] = reference
	a.record(key, p, packageID, digests["sha256"], uint32(info.Mode().Perm()))
	return nil
}

// modrinthRelease selects the catalogue release for the identified artifact.
func (a *packageAdopter) modrinthRelease(
	ctx context.Context,
	ref catalog.VersionedRef,
	ecosystem catalog.Ecosystem,
) (upstream.Candidate, error) {
	candidates, err := a.service.source.Candidates(
		ctx,
		catalog.RegistryModrinth,
		ref.Name.String(),
		string(a.manifest.Server.Minecraft),
		ecosystem,
	)
	if err != nil {
		return upstream.Candidate{}, err
	}
	index := slices.IndexFunc(candidates, func(candidate upstream.Candidate) bool {
		return candidate.Version == ref.Version && candidate.PrimaryFile
	})
	if index < 0 {
		return upstream.Candidate{}, fmt.Errorf(
			"no release provides version %s",
			ref.Version,
		)
	}
	return candidates[index], nil
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
	prior := ""
	if index < len(a.packages) {
		prior = a.packages[index].Path
		a.packages[index] = install.AdoptedPackage{}
	}
	a.recognized--
	return prior
}

func (a *packageAdopter) record(
	key string,
	p workspace.DiscoveredPackage,
	packageID string,
	sha256 string,
	mode uint32,
) {
	a.recognized++
	a.index[key] = len(a.packages)
	a.packages = append(a.packages, install.AdoptedPackage{
		Path:           p.Path,
		PackageID:      packageID,
		ExpectedSHA256: sha256,
		ExpectedMode:   mode,
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

func supportsModrinthPackages(ecosystem catalog.Ecosystem) bool {
	switch ecosystem {
	case catalog.EcosystemFabric,
		catalog.EcosystemForge,
		catalog.EcosystemNeoForge,
		catalog.EcosystemBukkit,
		catalog.EcosystemPaper:
		return true
	default:
		return false
	}
}
