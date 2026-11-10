package operation

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

// InitOptions describes adoption of the server already present in a
// directory.
type InitOptions struct {
	Name        string
	MCDRVersion string
	Force       bool
	NoComments  bool
	// Expectation narrows the observed installations by product and version
	// for this initialization only; it is never persisted.
	Expectation *discovery.Expectation
	// PromptRuntime asks which installation to use when observation finds
	// several equally valid runtimes. Nil keeps the strict failure.
	PromptRuntime func(discovery.Result) (*discovery.Installation, error)
	// Trace receives progress for the lock resolution initialization runs.
	Trace *install.Trace
	// Materialize supplies artifact bytes for adoption planning. Nil uses the
	// configured cache and network.
	Materialize func(*catalog.Artifact) (artifact.MaterializedFile, error)
}

// InitResult is the structured outcome of one initialization. The document
// is the written manifest; warnings and discovery carry the observations
// that the command boundary presents.
type InitResult struct {
	Document        *documentpkg.Manifest
	Lock            *documentpkg.Lock
	PublishedLock   bool
	PublishedConfig bool
	Warnings        []error
	Discovery       discovery.Result
}

func commandDocuments(root string, logger *log.Logger) (document *documentpkg.Manifest, locked *documentpkg.Lock, resultErr error) {
	writer, err := workspacewrite.Lock(root, logger)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := filetrack.RecoverLocked(root, logger); err != nil {
		return nil, nil, err
	}
	d, err := documentpkg.ManifestFile(root).Read()
	if err != nil {
		return nil, nil, err
	}
	old, err := documentpkg.LockFile(root).Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	return d, old, nil
}

// pinWorkspaceDocuments fills the state an operation selected from when the
// caller did not already pin it. The installation re-locks the workspace, so a
// recovery or concurrent edit in between fails instead of being overwritten.
func pinWorkspaceDocuments(opts *install.Options, d *documentpkg.Manifest, old *documentpkg.Lock) error {
	if d != nil && opts.ExpectedManifest == "" {
		fingerprint, err := d.Fingerprint()
		if err != nil {
			return err
		}
		opts.ExpectedManifest = fingerprint
	}
	if old == nil || opts.ExpectedLock != "" {
		return nil
	}
	fingerprint, err := old.Fingerprint()
	if err != nil {
		return err
	}
	opts.ExpectedLock = fingerprint
	return nil
}

func selectedEcosystem(d *documentpkg.Manifest, requested catalog.Ecosystem) catalog.Ecosystem {
	if requested != catalog.EcosystemUnspecified {
		return requested
	}
	switch d.Server.Core.Product {
	case serverpkg.ProductFabric:
		return catalog.EcosystemFabric
	case serverpkg.ProductQuilt:
		return catalog.EcosystemQuilt
	case serverpkg.ProductForge:
		return catalog.EcosystemForge
	case serverpkg.ProductNeoForge:
		return catalog.EcosystemNeoForge
	case serverpkg.ProductPaper, serverpkg.ProductPurpur:
		return catalog.EcosystemPaper
	}
	if d.Server.Core.Repository != "" {
		return catalog.EcosystemPaper
	}
	if d.MCDR != nil {
		return catalog.EcosystemMCDR
	}
	return catalog.EcosystemUnspecified
}

// servedEcosystemSet returns the package ecosystems that the declared server
// can load. A probed runtime also reports bridged ecosystems, such as Fabric
// through Sinytra Connector. Only ecosystems the declared product loads enter
// the set. Without a probe, the declared product decides.
func servedEcosystemSet(d *documentpkg.Manifest, observed *workspace.Workspace) map[catalog.Ecosystem]struct{} {
	target := resolve.Target{Product: d.Server.Core.Product}
	served := map[catalog.Ecosystem]struct{}{}
	if observed != nil {
		for _, offer := range observed.ServedEcosystems() {
			if resolve.SupportsPackages(target, offer.Ecosystem) {
				served[offer.Ecosystem] = struct{}{}
			}
		}
	}
	if len(served) == 0 {
		if ecosystem := selectedEcosystem(d, catalog.EcosystemUnspecified); ecosystem != catalog.EcosystemUnspecified {
			served[ecosystem] = struct{}{}
		}
	}
	return served
}

// servedEcosystemForFile reports the served loader whose descriptor the file
// carries. A jar without a top-level descriptor can declare its loader inside
// a JarJar container, while the upstream project label may name a loader the
// server does not offer.
func servedEcosystemForFile(
	ctx context.Context,
	path string,
	served map[catalog.Ecosystem]struct{},
) (catalog.Ecosystem, bool) {
	for _, ecosystem := range slices.Sorted(maps.Keys(served)) {
		if ecosystem == catalog.EcosystemMCDR {
			continue
		}
		modules, err := artifact.ReadModules(ctx, path, ecosystem)
		if err != nil {
			continue
		}
		for _, module := range modules {
			if loaderSpecificDescriptor(module) {
				return ecosystem, true
			}
		}
	}
	return catalog.EcosystemUnspecified, false
}

// loaderSpecificDescriptor reports whether a module's descriptor names the
// loader that read it. Forge and NeoForge share META-INF/mods.toml, so only a
// loader dependency declared inside it identifies the loader.
func loaderSpecificDescriptor(module catalog.DescriptorModule) bool {
	if module.DescriptorPath != "META-INF/mods.toml" {
		return true
	}
	loader := module.Ecosystem.String()
	return slices.ContainsFunc(module.Dependencies, func(dep catalog.DescriptorDependency) bool {
		return dep.ID == loader
	})
}

func commandReference(d *documentpkg.Manifest, old *documentpkg.Lock, raw string, ecosystem catalog.Ecosystem) (string, version.Version, error) {
	request, err := input.Parse(raw)
	if err != nil {
		return "", "", err
	}
	provider, name := request.Provider, string(request.Name)
	if provider == catalog.ProviderUnspecified {
		if old != nil {
			ref, err := old.LookupReference(name)
			if err != nil {
				return "", "", err
			}
			if ref != "" {
				provider, name, err = documentpkg.ParseReference(ref)
				if err != nil {
					return "", "", err
				}
			}
		}
		if provider == catalog.ProviderUnspecified {
			provider = catalog.ProviderModrinth
			if ecosystem == catalog.EcosystemMCDR {
				provider = catalog.ProviderMCDR
			}
		}
	}
	ref := provider.String() + ":" + name
	if _, _, err := documentpkg.ParseReference(ref); err != nil {
		return "", "", err
	}
	selector := request.Version
	if selector == "" {
		selector = version.Any
	}
	return ref, selector, nil
}

func (s *Service) Add(ctx context.Context, root string, args []string, ecosystem catalog.Ecosystem, opts install.Options) (*install.Result, error) {
	d, old, err := commandDocuments(root, s.logger)
	if err != nil {
		return nil, err
	}
	if err := pinWorkspaceDocuments(&opts, d, old); err != nil {
		return nil, err
	}
	opts.RequirementChange = true
	ecosystem = selectedEcosystem(d, ecosystem)
	if ecosystem == catalog.EcosystemUnspecified {
		return nil, fmt.Errorf("specify a package ecosystem supported by the declared server")
	}
	for _, raw := range args {
		ref, selector, err := commandReference(d, old, raw, ecosystem)
		if err != nil {
			return nil, err
		}
		provider, _, err := documentpkg.ParseReference(ref)
		if err != nil {
			return nil, err
		}
		// The manifest stores MCDR packages in a separate block. A package
		// provider that does not serve the selected environment would be
		// written where resolution can never select it.
		if (provider == catalog.ProviderMCDR) != (ecosystem == catalog.EcosystemMCDR) {
			return nil, fmt.Errorf(
				"provider %s cannot supply %s packages",
				provider,
				ecosystem,
			)
		}
		r := documentpkg.Requirement{Selector: selector, Enabled: new(true)}
		if ecosystem == catalog.EcosystemMCDR {
			if d.MCDR == nil {
				return nil, fmt.Errorf("manifest has no MCDR runtime")
			}
			d.MCDR.Packages[ref] = r
		} else {
			if d.Server.Packages[ecosystem] == nil {
				d.Server.Packages[ecosystem] = map[string]documentpkg.Requirement{}
			}
			d.Server.Packages[ecosystem][ref] = r
		}
	}
	opts.PublishManifest = true
	return s.install.Sync(ctx, root, d, old, opts)
}

func (s *Service) Remove(ctx context.Context, root string, args []string, ecosystem catalog.Ecosystem, opts install.Options) (*install.Result, error) {
	d, old, err := commandDocuments(root, s.logger)
	if err != nil {
		return nil, err
	}
	if err := pinWorkspaceDocuments(&opts, d, old); err != nil {
		return nil, err
	}
	opts.RequirementChange = true
	ecosystem = selectedEcosystem(d, ecosystem)
	for _, raw := range args {
		ref, _, err := commandReference(d, old, raw, ecosystem)
		if err != nil {
			return nil, err
		}
		packages := d.Server.Packages[ecosystem]
		if ecosystem == catalog.EcosystemMCDR {
			if d.MCDR == nil {
				return nil, fmt.Errorf("manifest has no MCDR runtime")
			}
			packages = d.MCDR.Packages
		}
		if _, ok := packages[ref]; !ok {
			return nil, fmt.Errorf("direct requirement %s not found in %s", ref, ecosystem)
		}
		delete(packages, ref)
	}
	opts.PublishManifest = true
	return s.install.Sync(ctx, root, d, old, opts)
}
