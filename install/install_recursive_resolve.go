package install

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
)

type candidateRequest struct {
	id             types.PackageId
	provenancePath []string
	mandatory      bool
}

// BuildCandidateGraph expands the recursive advisory dependency closure for the
// requested roots, seeding fixed installed constraints up front and running the
// constraint merge engine after every newly discovered dependency batch.
func BuildCandidateGraph(
	roots []types.PackageId,
	providers []upstream.Provider,
	installedConstraints []InstalledConstraint,
) (*RecursiveTransaction, error) {
	tx := NewRecursiveTransaction(roots, providers)
	tx.InstalledConstraints = append([]InstalledConstraint(nil), installedConstraints...)

	constraintInputs := make([]ConstraintInput, 0, len(installedConstraints))
	for _, installed := range installedConstraints {
		constraintInputs = append(constraintInputs, installed.ConstraintInput)
		if installed.Package.Id.Platform == "" || installed.Package.Id.Name == "" {
			continue
		}
		key := installed.Package.Id.StringPlatformName()
		if _, exists := tx.CandidateGraph[key]; exists {
			continue
		}
		tx.CandidateGraph[key] = CandidateNode{
			Package:        installed.Package,
			ProvenancePath: []string{installed.ConstraintInput.Requester},
			Advisory:       false,
		}
	}

	if _, err := MergeConstraintGraph(constraintInputs); err != nil {
		return nil, err
	}

	queue := make([]candidateRequest, 0, len(roots))
	for _, root := range roots {
		ReportCompatibleInstalled(tx, root)
		queue = append(queue, candidateRequest{
			id:             root,
			provenancePath: []string{"root"},
			mandatory:      true,
		})
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		key := current.id.StringPlatformName()
		if _, exists := tx.CandidateGraph[key]; exists {
			continue
		}

		pkg, err := resolveCandidatePackage(tx.Providers, current.id)
		if err != nil {
			if current.mandatory {
				return nil, err
			}
			continue
		}

		tx.CandidateGraph[key] = CandidateNode{
			Package:        pkg,
			ProvenancePath: append([]string(nil), current.provenancePath...),
			Advisory:       true,
		}

		dependencyProviders := providersForSource(tx.Providers, pkg.Remote)
		dependencySets, providerErrors := routing.DependenciesMany(dependencyProviders, pkg.Id)
		if len(dependencySets) == 0 {
			if current.mandatory {
				return nil, fmt.Errorf(
					"install: failed to resolve mandatory dependency %s: %s",
					pkg.Id.StringPlatformName(),
					formatProviderErrors(providerErrors),
				)
			}
			continue
		}

		batchInputs := make([]ConstraintInput, 0)
		children := make([]candidateRequest, 0)
		for _, dependencySet := range dependencySets {
			requester := current.id.StringFull()
			for _, dependency := range dependencySet.Value {
				batchInputs = append(batchInputs, ConstraintInput{
					Requester:  requester,
					Dependency: dependency,
				})

				childKey := dependency.Id.StringPlatformName()
				if _, exists := tx.CandidateGraph[childKey]; exists {
					continue
				}

				children = append(children, candidateRequest{
					id:             dependency.Id,
					provenancePath: appendPath(current.provenancePath, requester),
					mandatory:      dependency.Mandatory,
				})
			}
		}

		if len(batchInputs) > 0 {
			constraintInputs = append(constraintInputs, batchInputs...)
			if _, err := MergeConstraintGraph(constraintInputs); err != nil {
				return nil, err
			}
		}

		queue = append(queue, children...)
	}

	return tx, nil
}

func appendPath(path []string, requester string) []string {
	next := make([]string, 0, len(path)+1)
	next = append(next, path...)
	next = append(next, requester)
	return next
}

func formatProviderErrors(providerErrors []routing.ProviderError) string {
	if len(providerErrors) == 0 {
		return "no provider succeeded"
	}

	reasons := make([]string, 0, len(providerErrors))
	for _, providerErr := range providerErrors {
		reasons = append(reasons, providerErr.Error())
	}
	return strings.Join(reasons, "; ")
}

func resolveCandidatePackage(
	providers []upstream.Provider,
	id types.PackageId,
) (types.Package, error) {
	attempts := []types.PackageId{id}
	if id.Version == types.VersionCompatible {
		attempts = append(attempts,
			types.PackageId{Platform: id.Platform, Name: id.Name, Version: types.VersionLatest},
			types.PackageId{Platform: id.Platform, Name: id.Name, Version: types.VersionAny},
		)
	}

	var lastErrors []routing.ProviderError
	for _, attempt := range attempts {
		fetches, providerErrors := routing.FetchMany(providers, attempt)
		if len(fetches) == 0 {
			lastErrors = providerErrors
			continue
		}

		fetch := fetches[0]
		return types.Package{
			Id:     fetch.ResolvedID,
			Remote: &fetch.Remote,
		}, nil
	}

	return types.Package{}, fmt.Errorf(
		"install: failed to resolve mandatory dependency %s: %s",
		id.StringPlatformName(),
		formatProviderErrors(lastErrors),
	)
}

func providersForSource(
	providers []upstream.Provider,
	remote *types.PackageRemote,
) []upstream.Provider {
	if remote == nil {
		return providers
	}

	filtered := make([]upstream.Provider, 0, 1)
	for _, provider := range providers {
		if provider.Source() == remote.Source {
			filtered = append(filtered, provider)
		}
	}
	if len(filtered) == 0 {
		return providers
	}
	return filtered
}
