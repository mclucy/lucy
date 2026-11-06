package upstream

import (
	"fmt"

	"github.com/mclucy/lucy/catalog"
)

// ProviderError pairs a provider identity with the failure it reported.
type ProviderError struct {
	// Provider identifies the semantic upstream label for user-facing diagnostics.
	// The failed runtime executor is a Provider implementation.
	Provider catalog.Provider
	Err      error
}

func (e ProviderError) Error() string {
	return fmt.Sprintf("%s: %v", e.Provider.String(), e.Err)
}

func (e ProviderError) Unwrap() error {
	return e.Err
}

// ArtifactLookup is the result of a hash lookup on the upstream mappers.
// Ref is zero when no mapper matched the artifact. Errors holds the failure
// of each mapper. A lookup with errors is incomplete, not a miss.
type ArtifactLookup struct {
	Ref    catalog.VersionedRef
	Errors []ProviderError
}

// Matched reports whether some mapper identified the artifact.
func (l ArtifactLookup) Matched() bool { return l.Ref.Name != "" }

// Complete reports whether every attempted mapper finished without failure.
func (l ArtifactLookup) Complete() bool { return len(l.Errors) == 0 }
