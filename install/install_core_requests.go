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

type regularRootPolicy struct {
	Version types.BareVersion
	Source  types.SourceId
}

var coreBootstrapBindings = map[types.CorePackage]coreBootstrapBinding{
	types.CoreMinecraft: {
		Core:            types.CoreMinecraft,
		Ecosystem:       types.EcoMinecraft,
		InstallerSource: types.SourceMojang,
		AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceMojang},
		Tier:            0,
	},
	types.CoreFabric: {
		Core:            types.CoreFabric,
		Ecosystem:       types.EcoFabric,
		InstallerSource: types.SourceFabric,
		AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceFabric},
		Tier:            1,
	},
	types.CoreForge: {
		Core:            types.CoreForge,
		Ecosystem:       types.EcoForge,
		InstallerSource: types.SourceForge,
		AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceForge},
		Tier:            1,
	},
	types.CoreNeoForge: {
		Core:            types.CoreNeoForge,
		Ecosystem:       types.EcoNeoforge,
		InstallerSource: types.SourceNeoForge,
		AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceNeoForge},
		Tier:            1,
	},
	types.CoreMCDReforged: {
		Core:            types.CoreMCDReforged,
		Ecosystem:       types.EcoMcdr,
		InstallerSource: types.SourceMCDR,
		AcceptedSources: []types.SourceId{types.SourceAuto, types.SourceMCDR},
		Tier:            2,
	},
}

func classifyInstallRequests(
	requests []types.PackageRequest,
) ([]preparedCoreRequest, []types.PackageRequest) {
	cores := make([]preparedCoreRequest, 0, len(requests))
	regular := make([]types.PackageRequest, 0, len(requests))
	seen := make(map[types.FullPackageRef]struct{}, len(requests))

	for _, request := range requests {
		match, ok := types.NormalizeCorePackage(types.ScopedPackageRef{
			PackageRef: request.PackageRef,
			Scope:      request.Scope,
		})
		if ok {
			request.PackageRef = match.Ref.PackageRef
			request.Scope = match.Ref.Scope
		}

		if _, exists := seen[request.FullPackageRef]; exists {
			continue
		}
		seen[request.FullPackageRef] = struct{}{}

		if !ok {
			regular = append(regular, request)
			continue
		}
		cores = append(cores, preparedCoreRequest{
			Request: request,
			Match:   match,
		})
	}

	return cores, regular
}

func prepareCoreRequests(requests []preparedCoreRequest) error {
	tierOne := make([]types.CorePackage, 0, len(requests))
	policies := make(map[types.PackageRef]regularRootPolicy, len(requests))

	for i := range requests {
		binding, ok := coreBootstrapBindings[requests[i].Match.Core]
		if !ok {
			return fmt.Errorf(
				"unsupported core package: %s",
				requests[i].Match.Core,
			)
		}
		if !slices.Contains(
			binding.AcceptedSources,
			requests[i].Match.Ref.Scope,
		) {
			return fmt.Errorf(
				"core package %s does not accept source %s",
				requests[i].Match.Core,
				requests[i].Match.Ref.Scope,
			)
		}
		requests[i].Binding = binding

		if binding.Tier == 1 {
			tierOne = append(tierOne, binding.Core)
		}

		ref := requests[i].Match.Ref.PackageRef
		policy := regularRootPolicy{
			Version: requests[i].Request.Version,
			Source:  requests[i].Request.Scope,
		}
		if existing, exists := policies[ref]; exists && existing != policy {
			return fmt.Errorf(
				"conflicting core package requests for %s: %s@%s and %s@%s",
				ref.StringBase(),
				existing.Source,
				existing.Version,
				policy.Source,
				policy.Version,
			)
		}
		policies[ref] = policy
	}

	if len(tierOne) > 1 {
		return fmt.Errorf(
			"incompatible core packages %v: only one mod loader is allowed",
			tierOne,
		)
	}

	slices.SortStableFunc(requests, func(a, b preparedCoreRequest) int {
		return a.Binding.Tier - b.Binding.Tier
	})
	return nil
}

func prepareRegularRoots(
	requests []types.PackageRequest,
	defaultEcosystem types.Ecosystem,
) ([]types.PackageRequest, []types.VersionedPackageRef, error) {
	effective := make([]types.PackageRequest, 0, len(requests))
	roots := make([]types.VersionedPackageRef, 0, len(requests))
	policies := make(map[string]regularRootPolicy, len(requests))

	for _, request := range requests {
		if request.Eco == types.EcoUnspecified &&
			defaultEcosystem != types.EcoUnspecified {
			request.Eco = defaultEcosystem
		}

		root := types.VersionedPackageRef{
			PackageRef: request.PackageRef,
			Version:    request.Version,
		}
		key := root.StringBase()
		policy := regularRootPolicy{
			Version: request.Version,
			Source:  request.Scope,
		}
		if existing, exists := policies[key]; exists {
			if existing != policy {
				return nil, nil, fmt.Errorf(
					"install: conflicting requests for %s: %s@%s and %s@%s",
					key,
					existing.Source,
					existing.Version,
					policy.Source,
					policy.Version,
				)
			}
		} else {
			policies[key] = policy
			roots = append(roots, root)
		}
		effective = append(effective, request)
	}

	return effective, roots, nil
}

func coreRequestIDs(requests []preparedCoreRequest) []types.VersionedPackageRef {
	ids := make([]types.VersionedPackageRef, 0, len(requests))
	for _, request := range requests {
		ids = append(ids, types.VersionedPackageRef{
			PackageRef: request.Match.Ref.PackageRef,
			Version:    request.Request.Version,
		})
	}
	return ids
}

func batchDefaultEcosystem(
	cores []preparedCoreRequest,
	current types.Ecosystem,
) types.Ecosystem {
	for _, request := range cores {
		if request.Binding.Tier == 1 {
			return request.Binding.Ecosystem
		}
	}
	return current
}
