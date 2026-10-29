package operation

import (
	"context"
	"errors"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/version"
)

// Source is the configured routing capability that workspace operations
// consume: catalogue builds, artifact mappers, and build identification.
type Source interface {
	Candidates(
		ctx context.Context,
		registry catalog.Registry,
		project, minecraft string,
		ecosystem catalog.Ecosystem,
	) ([]upstream.Candidate, error)
	GetArtifactMapper(provider catalog.Provider) (upstream.ArtifactMapProvider, bool, error)
	IdentifyFillBuild(
		ctx context.Context,
		project string,
		minecraft version.Version,
		sha256 string,
	) (version.Version, bool, error)
	IdentifyPurpurBuild(
		ctx context.Context,
		minecraft version.Version,
		md5 string,
	) (version.Version, bool, error)
	ResolveArtifactByHash(
		ctx context.Context,
		artifact upstream.ArtifactFingerprint,
	) routing.ArtifactLookup
}

// Service executes workspace operations with explicit installation, routing,
// registry, and diagnostic dependencies. It never discovers a machine-global
// service of its own.
type Service struct {
	install  *install.Service
	source   Source
	registry *serverregistry.Registry
	logger   *log.Logger
}

// NewService constructs the operation service. registry may be nil when the
// machine has no registry location; operations report that only when they
// actually need registration state.
func NewService(
	installer *install.Service,
	source Source,
	registry *serverregistry.Registry,
	logger *log.Logger,
) *Service {
	return &Service{
		install:  installer,
		source:   source,
		registry: registry,
		logger:   logger,
	}
}

// registerWorkspace records root for the artifact store's retention sweep.
// Operations that publish a lock must not report success when the entry is
// missing, because the sweep could otherwise reclaim that lock's artifacts.
func (s *Service) registerWorkspace(root string) ([]serverregistry.Entry, error) {
	if s.registry == nil {
		return nil, errors.New("server registry is unavailable; cannot record this workspace for artifact retention")
	}
	return s.registry.Register(root)
}
