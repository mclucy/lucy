package install

import (
	"fmt"
	"slices"

	"github.com/mclucy/lucy/types"
)

type coreBootstrapBinding struct {
	Core            types.CorePackage
	Ecosystem       types.Ecosystem
	InstallerSource types.SourceId
	AcceptedSources []types.SourceId
	Tier            int
}

type preparedCoreRequest struct {
	Request types.PackageRequest
	Match   types.CorePackageMatch
	Binding coreBootstrapBinding
}

type requestPolicy struct {
	Version types.BareVersion
	Source  types.SourceId
}

var coreBootstrapBindings = map[types.CorePackage]coreBootstrapBinding{
	types.CoreMinecraft:   {Core: types.CoreMinecraft, Ecosystem: types.EcoMinecraft, InstallerSource: types.SourceMojang, AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceMojang}, Tier: 0},
	types.CoreFabric:      {Core: types.CoreFabric, Ecosystem: types.EcoFabric, InstallerSource: types.SourceFabric, AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceFabric}, Tier: 1},
	types.CoreForge:       {Core: types.CoreForge, Ecosystem: types.EcoForge, InstallerSource: types.SourceForge, AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceForge}, Tier: 1},
	types.CoreNeoForge:    {Core: types.CoreNeoForge, Ecosystem: types.EcoNeoforge, InstallerSource: types.SourceNeoForge, AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceNeoForge}, Tier: 1},
	types.CoreMCDReforged: {Core: types.CoreMCDReforged, Ecosystem: types.EcoMcdr, InstallerSource: types.SourceMCDR, AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceMCDR}, Tier: 2},
}

func classifyInstallRequests(requests []types.PackageRequest) ([]preparedCoreRequest, []types.PackageRequest) {
	cores := make([]preparedCoreRequest, 0, len(requests))
	regular := make([]types.PackageRequest, 0, len(requests))
	seen := make(map[types.PackageRequest]struct{}, len(requests))
	for _, request := range requests {
		if _, exists := seen[request]; exists {
			continue
		}
		seen[request] = struct{}{}
		match, ok := types.NormalizeCorePackage(request)
		if !ok {
			regular = append(regular, request)
			continue
		}
		cores = append(cores, preparedCoreRequest{Request: request, Match: match})
	}
	return cores, regular
}

func prepareCoreRequests(requests []preparedCoreRequest) error {
	tierOne := make([]types.CorePackage, 0, len(requests))
	policies := make(map[types.CorePackage]requestPolicy, len(requests))
	for i := range requests {
		binding, ok := coreBootstrapBindings[requests[i].Match.Core]
		if !ok {
			return fmt.Errorf("unsupported core package: %s", requests[i].Match.Core)
		}
		if !slices.Contains(binding.AcceptedSources, requests[i].Request.Source) {
			return fmt.Errorf("core package %s does not accept source %s", requests[i].Match.Core, requests[i].Request.Source)
		}
		requests[i].Binding = binding
		if binding.Tier == 1 {
			tierOne = append(tierOne, binding.Core)
		}
		policy := requestPolicy{Version: requests[i].Request.Version, Source: requests[i].Request.Source}
		if existing, exists := policies[requests[i].Match.Core]; exists && existing != policy {
			return fmt.Errorf("conflicting core package requests for %s", requests[i].Match.Core)
		}
		policies[requests[i].Match.Core] = policy
	}
	if len(tierOne) > 1 {
		return fmt.Errorf("incompatible core packages %v: only one mod loader is allowed", tierOne)
	}
	slices.SortStableFunc(requests, func(a, b preparedCoreRequest) int { return a.Binding.Tier - b.Binding.Tier })
	return nil
}

func prepareRegularRoots(requests []types.PackageRequest, defaultEcosystem types.Ecosystem) ([]types.PackageRequest, []types.VersionedPackageRef, error) {
	effective := make([]types.PackageRequest, 0, len(requests))
	roots := make([]types.VersionedPackageRef, 0, len(requests))
	policies := make(map[types.PackageRef]requestPolicy, len(requests))
	for _, request := range requests {
		if request.Eco == types.EcoUnspecified {
			request.Eco = defaultEcosystem
		}
		if request.Eco == types.EcoUnspecified {
			return nil, nil, fmt.Errorf("install: no target ecosystem for %s", request.Name)
		}
		root := types.VersionedPackageRef{PackageRef: request.PackageRef, Eco: request.Eco, Version: request.Version}
		policy := requestPolicy{Version: request.Version, Source: request.Source}
		if existing, exists := policies[request.PackageRef]; exists && existing != policy {
			return nil, nil, fmt.Errorf("install: conflicting requests for %s", request.PackageRef.StringBase())
		}
		if _, exists := policies[request.PackageRef]; !exists {
			roots = append(roots, root)
		}
		policies[request.PackageRef] = policy
		effective = append(effective, request)
	}
	return effective, roots, nil
}

func coreRequestIDs(requests []preparedCoreRequest) []types.VersionedPackageRef {
	ids := make([]types.VersionedPackageRef, 0, len(requests))
	for _, request := range requests {
		ids = append(ids, types.VersionedPackageRef{PackageRef: request.Request.PackageRef, Eco: request.Binding.Ecosystem, Version: request.Request.Version})
	}
	return ids
}

func batchDefaultEcosystem(cores []preparedCoreRequest, current types.Ecosystem) types.Ecosystem {
	for _, request := range cores {
		if request.Binding.Tier == 1 {
			return request.Binding.Ecosystem
		}
	}
	return current
}
