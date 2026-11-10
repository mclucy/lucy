package operation

import (
	"context"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/workspace/discovery"
)

// ImportOptions describes one external server source and its target workspace.
// Directory must be an absolute path that is absent or empty.
type ImportOptions struct {
	Source        string
	Directory     string
	Name          string
	MCDRVersion   string
	NoComments    bool
	SHA256        string
	Expectation   *discovery.Expectation
	PromptRuntime func(discovery.Result) (*discovery.Installation, error)
	Trace         *install.Trace
}

// ImportResult reports the materialized source and the adopted workspace.
type ImportResult struct {
	Workspace       string             `json:"workspace"`
	Source          string             `json:"source"`
	Format          string             `json:"format"`
	Name            string             `json:"name"`
	Minecraft       string             `json:"minecraft"`
	Product         string             `json:"product"`
	CoreVersion     string             `json:"core_version"`
	ServerFiles     int                `json:"server_files"`
	SkippedClient   int                `json:"skipped_client"`
	Overrides       int                `json:"overrides"`
	ManagedPackages int                `json:"managed_packages"`
	Skipped         []string           `json:"skipped,omitempty"`
	Unmanaged       []string           `json:"unmanaged,omitempty"`
	Review          []string           `json:"review,omitempty"`
	WarningMessages []string           `json:"warnings,omitempty"`
	Manifest        *document.Manifest `json:"-"`
	Lock            *document.Lock     `json:"-"`
	Warnings        []error            `json:"-"`
	Discovery       discovery.Result   `json:"-"`
}

// Import materializes source, installs a declared loader when needed, adopts
// the resulting tree, and publishes the workspace at Directory.
func (s *Service) Import(
	ctx context.Context,
	opts ImportOptions,
	network *cache.Client,
) (*ImportResult, error) {
	return s.importSource(ctx, opts, network)
}
