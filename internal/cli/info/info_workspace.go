package info

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

func infoWorkingDirectory() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", &report.Detail{
			Title: "cannot determine the working directory",
			Lines: []string{err.Error()},
			Code:  infoErrorWorkspace,
			Cause: err,
		}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", &report.Detail{
			Title: "cannot resolve the working directory",
			Lines: []string{err.Error()},
			Code:  infoErrorWorkspace,
			Cause: err,
		}
	}
	return filepath.Clean(absolute), nil
}

// infoObserveWorkspace is a read-only observation used for installed facts and
// for the version-resolution context. Failure to observe is not an info
// failure: the project document still renders without local facts.
func infoObserveWorkspace(root string) *workspace.Workspace {
	if root == "" {
		return nil
	}
	return workspace.Observe(
		root,
		discovery.Request{Policy: discovery.PolicyPreferModified},
	)
}

// infoReadLock reports the workspace lock when one is readable. A missing or
// invalid lock only removes lock-derived matching; it never fails info.
func infoReadLock(root string) *document.Lock {
	if root == "" {
		return nil
	}
	lock, err := document.LockFile(root).Read()
	if err != nil {
		return nil
	}
	return lock
}

// infoInstalledAt reports an installed package only when the workspace
// contains the actual file. The lock may identify which file belongs to the
// project, but it never proves installation by itself.
func infoInstalledAt(
	root string,
	request catalog.Ref,
	observed *workspace.Workspace,
	lock *document.Lock,
) *infoInstalled {
	if observed == nil || len(observed.Packages) == 0 {
		return nil
	}

	locked := infoLockProjectPackages(lock, request)
	matches := make([]workspace.DiscoveredPackage, 0, len(observed.Packages))
	matchedLock := make([]*document.Package, 0, len(observed.Packages))

	// A lock binding names the project; the recorded filename ties that
	// binding to an observed file even when the descriptor uses another id.
	for _, lockedPackage := range locked {
		filename := filepath.Base(lockedPackage.Filename)
		if filename == "" || filename == "." {
			continue
		}
		for _, observedPackage := range observed.Packages {
			if filepath.Base(observedPackage.Path) != filename {
				continue
			}
			matches = append(matches, observedPackage)
			matchedLock = append(matchedLock, lockedPackage)
		}
	}

	if len(matches) == 0 {
		var identityLock *document.Package
		if len(locked) > 0 {
			identityLock = locked[0]
		}
		for _, observedPackage := range observed.Packages {
			if !infoInstalledNameMatches(request, observedPackage.ID) {
				continue
			}
			matches = append(matches, observedPackage)
			matchedLock = append(matchedLock, identityLock)
		}
	}
	if len(matches) == 0 {
		return nil
	}

	if len(matches) > 1 {
		identityMatches := make([]workspace.DiscoveredPackage, 0, len(matches))
		identityLocks := make([]*document.Package, 0, len(matches))
		for index, match := range matches {
			if !infoInstalledNameMatches(request, match.ID) {
				continue
			}
			identityMatches = append(identityMatches, match)
			identityLocks = append(identityLocks, matchedLock[index])
		}
		if len(identityMatches) > 0 {
			matches = identityMatches
			matchedLock = identityLocks
		}
	}

	best := 0
	for index := 1; index < len(matches); index++ {
		if matches[index].Path < matches[best].Path {
			best = index
		}
	}
	installed := matches[best]
	lockedPackage := matchedLock[best]
	versionText := infoVersionText(installed.ID.Version)
	for _, match := range matches {
		if infoVersionText(match.ID.Version) != versionText {
			versionText = ""
			break
		}
	}

	return &infoInstalled{
		Ref:     infoInstalledRef(installed, lockedPackage, request),
		Version: versionText,
		Path:    infoCleanPath(terminal.DisplayPath(root, installed.Path)),
	}
}

// infoInstalledRef qualifies the installed ref with the upstream project the
// lock or request matched. A local descriptor id may differ from that project
// slug, so it must not be paired with an inferred provider. Without upstream
// attribution the ref stays an honest, unqualified local identity.
func infoInstalledRef(
	installed workspace.DiscoveredPackage,
	lockedPackage *document.Package,
	request catalog.Ref,
) string {
	if lockedPackage != nil {
		provider, name := infoLockPackageIdentity(lockedPackage, request)
		if name != "" {
			if !infoProviderUsable(provider) &&
				infoProviderUsable(request.Provider) {
				provider = request.Provider
			}
			return infoProjectRef(name, provider, version.Any)
		}
	}
	if infoProviderUsable(request.Provider) {
		return infoProjectRef(request.Name.String(), request.Provider, version.Any)
	}
	return infoProjectRef(
		installed.ID.Name.String(),
		catalog.ProviderUnspecified,
		version.Any,
	)
}

func infoLockProjectPackages(
	lock *document.Lock,
	request catalog.Ref,
) []*document.Package {
	if lock == nil {
		return nil
	}
	matches := make([]*document.Package, 0, len(lock.Packages))
	for index := range lock.Packages {
		lockedPackage := &lock.Packages[index]
		if infoLockPackageMatches(lockedPackage, request) {
			matches = append(matches, lockedPackage)
		}
	}
	return matches
}

func infoLockPackageMatches(
	lockedPackage *document.Package,
	request catalog.Ref,
) bool {
	for _, binding := range lockedPackage.Requirements {
		provider, project, err := document.ParseReference(binding.Reference)
		if err != nil {
			continue
		}
		if infoProjectIdentityMatches(request, provider, project) {
			return true
		}
	}
	provider := lockedPackage.Provider
	if !infoProviderUsable(provider) {
		provider = request.Provider
	}
	if infoProviderUsable(request.Provider) &&
		infoProviderUsable(provider) &&
		request.Provider != provider {
		return false
	}
	return infoNormalizedName(lockedPackage.ProjectID) ==
		infoNormalizedName(request.Name.String())
}

func infoProjectIdentityMatches(
	request catalog.Ref,
	provider catalog.Provider,
	project string,
) bool {
	if infoProviderUsable(request.Provider) &&
		infoProviderUsable(provider) &&
		request.Provider != provider {
		return false
	}
	return infoNormalizedName(project) ==
		infoNormalizedName(request.Name.String())
}

func infoInstalledNameMatches(
	request catalog.Ref,
	identity catalog.VersionedRef,
) bool {
	if infoNormalizedName(identity.Name.String()) !=
		infoNormalizedName(request.Name.String()) {
		return false
	}
	if !infoProviderUsable(request.Provider) {
		return true
	}
	if !infoProviderUsable(identity.Provider) {
		return true
	}
	return request.Provider == identity.Provider
}

func infoLockPackageProvider(
	lockedPackage *document.Package,
) catalog.Provider {
	if infoProviderUsable(lockedPackage.Provider) {
		return lockedPackage.Provider
	}
	provider := catalog.ProviderUnspecified
	for _, binding := range lockedPackage.Requirements {
		candidate, _, err := document.ParseReference(binding.Reference)
		if err != nil || !infoProviderUsable(candidate) {
			continue
		}
		if provider != catalog.ProviderUnspecified && provider != candidate {
			return catalog.ProviderUnspecified
		}
		provider = candidate
	}
	return provider
}

// infoLockPackageIdentity reports the project identity this lock package
// satisfies for the request. A binding that names the request wins over the
// package's opaque provider project id.
func infoLockPackageIdentity(
	lockedPackage *document.Package,
	request catalog.Ref,
) (catalog.Provider, string) {
	for _, binding := range lockedPackage.Requirements {
		provider, project, err := document.ParseReference(binding.Reference)
		if err != nil {
			continue
		}
		if infoProjectIdentityMatches(request, provider, project) {
			return provider, project
		}
	}
	return infoLockPackageProvider(lockedPackage), lockedPackage.ProjectID
}

// infoResolveProjectVersion turns an explicit version request into a concrete
// version only through the provider's context-aware catalogue. Anything less
// stays unpinned and produces a warning.
func infoResolveProjectVersion(
	ctx context.Context,
	request catalog.VersionedRef,
	provider catalog.Provider,
	observed *workspace.Workspace,
	lock *document.Lock,
) (version.Version, []string, error) {
	if request.Version == "" || request.Version == version.Any {
		return version.Any, nil, nil
	}

	registry, ok := infoCandidateRegistry(provider)
	if !ok {
		return version.Any, []string{fmt.Sprintf(
			"%s has no version catalogue; version %s was not resolved, so the project ref stays unpinned",
			infoProviderLabel(provider),
			request.Version.String(),
		)}, nil
	}

	minecraft := ""
	if selection := infoRuntimeSelection(observed); selection != nil {
		candidate := selection.MinecraftVersion()
		if infoVersionText(candidate) != "" {
			minecraft = candidate.String()
		}
	}
	if minecraft == "" && lock != nil &&
		infoVersionText(lock.Server.Minecraft) != "" {
		minecraft = lock.Server.Minecraft.String()
	}

	loader := request.Ecosystem
	if loader == catalog.EcosystemUnspecified {
		loader = infoWorkspaceLoader(observed, lock)
	}
	if provider == catalog.ProviderMCDR {
		loader = catalog.EcosystemMCDR
	}
	if provider != catalog.ProviderMCDR && minecraft == "" {
		return version.Any, []string{fmt.Sprintf(
			"no workspace Minecraft version is available to resolve version %s, so the project ref stays unpinned",
			request.Version.String(),
		)}, nil
	}

	type candidateResult struct {
		candidates []upstream.Candidate
		err        error
	}
	projectName := request.Name.String()
	if provider == catalog.ProviderMCDR {
		projectName = request.Name.PythonName()
	}
	result, err := infoAwait(ctx, func() candidateResult {
		candidates, lookupErr := routing.Candidates(
			ctx,
			registry,
			projectName,
			minecraft,
			loader,
		)
		return candidateResult{candidates: candidates, err: lookupErr}
	})
	if err != nil {
		if failure := infoOperationFailure(ctx, err); failure != nil {
			return version.Any, nil, failure
		}
		return version.Any, nil, err
	}
	if result.err != nil {
		if failure := infoOperationFailure(ctx, result.err); failure != nil {
			return version.Any, nil, failure
		}
		return version.Any, []string{fmt.Sprintf(
			"could not resolve version %s on %s: %v; the project ref stays unpinned",
			request.Version.String(),
			infoProviderLabel(provider),
			result.err,
		)}, nil
	}

	selected := infoSelectCandidate(result.candidates, request.Version)
	if infoVersionText(selected) == "" {
		return version.Any, []string{fmt.Sprintf(
			"version %s was not found among the builds available for this workspace, so the project ref stays unpinned",
			request.Version.String(),
		)}, nil
	}
	return selected, nil, nil
}

func infoWorkspaceLoader(observed *workspace.Workspace, lock *document.Lock) catalog.Ecosystem {
	if observed != nil {
		if loader := infoLoaderFromOffers(observed.ServedEcosystems()); loader != catalog.EcosystemUnspecified {
			return loader
		}
	}
	if lock == nil {
		return catalog.EcosystemUnspecified
	}
	components := make([]catalog.VersionedRef, 0)
	for _, input := range lock.Server.Inputs {
		for _, module := range input.Modules {
			components = append(components, catalog.VersionedRef{
				Name:      catalog.Name(module.ID),
				Version:   module.Version,
				Ecosystem: module.Ecosystem,
			})
		}
	}
	offers := workspace.ProductServedEcosystems(
		catalog.VersionedRef{
			Name:    catalog.Name(lock.Server.Product.String()),
			Version: lock.Server.CoreVersion,
		},
		components,
	)
	if loader := infoLoaderFromOffers(offers); loader != catalog.EcosystemUnspecified {
		return loader
	}
	for _, module := range components {
		if module.Ecosystem.IsModding() {
			return module.Ecosystem
		}
	}
	return catalog.EcosystemUnspecified
}

func infoLoaderFromOffers(offers []workspace.ServedEcosystem) catalog.Ecosystem {
	for _, offer := range offers {
		if offer.Compatibility == workspace.CompatFull &&
			offer.Ecosystem != catalog.EcosystemMinecraft &&
			offer.Ecosystem != catalog.EcosystemUnspecified {
			return offer.Ecosystem
		}
	}
	for _, offer := range offers {
		if offer.Ecosystem != catalog.EcosystemMinecraft &&
			offer.Ecosystem != catalog.EcosystemUnspecified {
			return offer.Ecosystem
		}
	}
	return catalog.EcosystemUnspecified
}

func infoCandidateRegistry(
	provider catalog.Provider,
) (catalog.Registry, bool) {
	switch provider {
	case catalog.ProviderModrinth:
		return catalog.RegistryModrinth, true
	case catalog.ProviderCurseForge:
		return catalog.RegistryCurseForge, true
	case catalog.ProviderMCDR:
		return catalog.RegistryMCDR, true
	default:
		return catalog.RegistryUnspecified, false
	}
}

func infoRuntimeSelection(
	observed *workspace.Workspace,
) *discovery.Selection {
	if observed == nil {
		return nil
	}
	selection := observed.Runtime()
	if selection == nil || !selection.IsValid() {
		return nil
	}
	return selection
}

func infoSelectCandidate(
	candidates []upstream.Candidate,
	requested version.Version,
) version.Version {
	switch requested {
	case version.Stable:
		return infoNewestCandidate(
			candidates,
			func(channel upstream.ReleaseChannel) bool {
				return channel == upstream.ReleaseChannelRelease
			},
		)
	case version.Beta:
		return infoNewestCandidate(
			candidates,
			func(upstream.ReleaseChannel) bool { return true },
		)
	case version.Version("latest"):
		if selected := infoNewestCandidate(
			candidates,
			func(channel upstream.ReleaseChannel) bool {
				return channel == upstream.ReleaseChannelRelease
			},
		); infoVersionText(selected) != "" {
			return selected
		}
		return infoNewestCandidate(
			candidates,
			func(upstream.ReleaseChannel) bool { return true },
		)
	default:
		requestedText := strings.TrimSpace(requested.String())
		for _, candidate := range candidates {
			if infoVersionText(candidate.Version) == "" {
				continue
			}
			if strings.TrimSpace(candidate.Version.String()) == requestedText {
				return candidate.Version
			}
		}
		return ""
	}
}

func infoNewestCandidate(
	candidates []upstream.Candidate,
	accept func(upstream.ReleaseChannel) bool,
) version.Version {
	var newest *upstream.Candidate
	for index := range candidates {
		candidate := &candidates[index]
		if infoVersionText(candidate.Version) == "" ||
			!accept(candidate.Channel) {
			continue
		}
		if newest == nil || candidate.PublishedAt.After(newest.PublishedAt) {
			newest = candidate
		}
	}
	if newest == nil {
		return version.Any
	}
	return newest.Version
}

func infoProviderLabel(provider catalog.Provider) string {
	if !infoProviderUsable(provider) {
		return "the provider"
	}
	return provider.Title()
}
