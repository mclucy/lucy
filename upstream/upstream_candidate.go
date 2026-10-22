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
}

// ProjectDependency identifies one upstream project that a candidate declares.
type ProjectDependency struct {
	Provider  catalog.Provider       `json:"provider"`
	ProjectID string                 `json:"project_id"`
	ReleaseID string                 `json:"release_id,omitempty"`
	Kind      catalog.DependencyKind `json:"kind"`
}
