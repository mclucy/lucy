package upstream

import (
	"time"

	"github.com/mclucy/lucy/catalog"
)

// Candidate is one published artifact that can satisfy an upstream selection.
type Candidate struct {
	catalog.Artifact
	Channel      ReleaseChannel
	PublishedAt  time.Time
	PrimaryFile  bool
	Dependencies []ProjectDependency

	// DependenciesUnknown reports that the provider could not obtain
	// dependency metadata for this build. An empty Dependencies slice then
	// means the provider did not report dependencies, not that the artifact
	// declares none. Resolution reports the gap instead of claiming the build
	// has no dependencies.
	DependenciesUnknown bool
	// CompatibilityUnknown reports that the provider could not verify this
	// build against the requested Minecraft version and ecosystem. Resolution
	// reports the gap instead of claiming the build is compatible.
	CompatibilityUnknown bool
}

// ProjectDependency identifies one upstream project that a candidate declares.
type ProjectDependency struct {
	Provider  catalog.Provider       `json:"provider"`
	ProjectID string                 `json:"project_id"`
	ReleaseID string                 `json:"release_id,omitempty"`
	Kind      catalog.DependencyKind `json:"kind"`
}
