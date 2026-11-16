package install

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

// TestMutateLockBindsEachDeclarationToLocation covers a reference declared for
// two ecosystems. Each locked package must receive its own declaration instead
// of the last one applied to every package that names the same reference.
func TestMutateLockBindsEachDeclarationToLocation(t *testing.T) {
	initial := lockedMutationManifest(t, "modrinth:shared", versionpkg.Any)
	initialFingerprint, err := initial.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: initialFingerprint,
		Server: document.LockedServer{
			Directory:   ".",
			Minecraft:   versionpkg.Version("1.21.4"),
			Product:     serverpkg.ProductPaper,
			CoreVersion: versionpkg.Version("1.21.4"),
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderPaperMC,
				ProjectID: "paper",
				Version:   versionpkg.Version("1.21.4"),
				Filename:  "paper.jar",
				Hashes:    map[string]string{"sha256": fmt.Sprintf("%064x", 9)},
			},
			Bootstrapper: "paperclip",
			Java:         document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{
			lockedMutationPackage(catalog.EcosystemFabric, versionpkg.Any, true),
			lockedMutationPackage(catalog.EcosystemPaper, versionpkg.Any, true),
		},
	}

	fabricSelector := versionpkg.Version("1.0.0")
	disabled := false
	final := lockedMutationManifest(t, "modrinth:shared", fabricSelector)
	final.Server.Packages[catalog.EcosystemPaper] = map[string]document.Requirement{
		"modrinth:shared": {Selector: versionpkg.Any, Enabled: &disabled},
	}
	fingerprint, err := final.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	manifestBefore := fingerprint
	lockBefore, err := lock.Fingerprint()
	if err != nil {
		t.Fatalf("lockfile.Fingerprint() error = %v", err)
	}
	resolved, err := mutateLock(final, lock, fingerprint)
	if err != nil {
		t.Fatalf("mutateLock() error = %v", err)
	}
	manifestAfter, err := final.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	if manifestBefore != manifestAfter {
		t.Fatal("mutateLock() mutated its input manifest")
	}
	lockAfter, err := lock.Fingerprint()
	if err != nil {
		t.Fatalf("lockfile.Fingerprint() error = %v", err)
	}
	if lockBefore != lockAfter {
		t.Fatal("mutateLock() mutated its input lock")
	}
	if resolved.ManifestFingerprint != fingerprint {
		t.Fatalf(
			"resolved lock answers manifest %q, want %q",
			resolved.ManifestFingerprint,
			fingerprint,
		)
	}

	want := map[catalog.Ecosystem]document.RequirementBinding{
		catalog.EcosystemFabric: {
			Reference: "modrinth:shared",
			Selector:  fabricSelector,
			Enabled:   true,
		},
		catalog.EcosystemPaper: {
			Reference: "modrinth:shared",
			Selector:  versionpkg.Any,
			Enabled:   false,
		},
	}
	if len(resolved.Packages) != len(want) {
		t.Fatalf("resolved packages = %d, want %d: %+v", len(resolved.Packages), len(want), resolved.Packages)
	}
	for _, pkg := range resolved.Packages {
		wantBinding, ok := want[pkg.Ecosystem]
		if !ok {
			t.Fatalf("resolved unexpected ecosystem %q", pkg.Ecosystem)
		}
		if pkg.Environment != serverpkg.EnvironmentServer {
			t.Fatalf(
				"resolved %s environment = %q, want %q",
				pkg.Ecosystem,
				pkg.Environment,
				serverpkg.EnvironmentServer,
			)
		}
		if !reflect.DeepEqual(pkg.Requirements, []document.RequirementBinding{wantBinding}) {
			t.Fatalf(
				"resolved %s requirements = %#v, want %#v",
				pkg.Ecosystem,
				pkg.Requirements,
				[]document.RequirementBinding{wantBinding},
			)
		}
		if pkg.Enabled() != wantBinding.Enabled {
			t.Fatalf(
				"resolved %s Enabled() = %v, want %v",
				pkg.Ecosystem,
				pkg.Enabled(),
				wantBinding.Enabled,
			)
		}
	}
}

// TestMutateLockBindsRecordedRequirement covers a locked declaration whose
// reference cannot be recovered from the pin: Modrinth names the project by an
// opaque ID and the jar's module ID need not equal the project slug. The
// requirement binding the earlier resolution recorded is the only evidence
// that the pin answers modrinth:ferrite-core, so a locked removal of another
// package must not report it as unpinned. The Quilt product mirrors the server
// integration case that exposed it.
func TestMutateLockBindsRecordedRequirement(t *testing.T) {
	const reference = "modrinth:ferrite-core"
	selector := versionpkg.Version("7.1.3-fabric")
	initial := lockedMutationManifest(t, reference, selector)
	initial.Server.Core.Product = serverpkg.ProductQuilt
	initialFingerprint, err := initial.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}

	pkg := lockedMutationPackage(catalog.EcosystemFabric, selector, true)
	pkg.ProjectID = "uXXizFIs"
	pkg.ReleaseID = "7KqeXPRS"
	pkg.FileID = "Rr0mcpKp"
	pkg.Version = selector
	pkg.Filename = "ferritecore-7.1.3-fabric.jar"
	pkg.Modules = []catalog.DescriptorModule{{
		ID:             "ferritecore",
		Version:        versionpkg.Version("7.1.3"),
		Ecosystem:      catalog.EcosystemFabric,
		VersionDialect: versionpkg.DialectFabric,
	}}
	pkg.Requirements = []document.RequirementBinding{{
		Reference: reference,
		Selector:  selector,
		Enabled:   true,
	}}
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: initialFingerprint,
		Server: document.LockedServer{
			Directory:   "server",
			Minecraft:   versionpkg.Version("1.21.4"),
			Product:     serverpkg.ProductQuilt,
			CoreVersion: versionpkg.Version("0.29.1"),
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderQuilt,
				ProjectID: "quilt-loader",
				Version:   versionpkg.Version("0.29.1"),
				Filename:  "quilt-loader-0.29.1.jar",
				Hashes:    map[string]string{"sha256": fmt.Sprintf("%064x", 8)},
			},
			Bootstrapper: serverpkg.BootstrapMethodQuilt,
			Java:         document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{pkg},
	}

	resolved, err := mutateLock(initial, lock, initialFingerprint)
	if err != nil {
		t.Fatalf("mutateLock() error = %v", err)
	}
	if len(resolved.Packages) != 1 {
		t.Fatalf("resolved packages = %d, want 1: %+v", len(resolved.Packages), resolved.Packages)
	}
	got := resolved.Packages[0]
	if got.ProjectID != "uXXizFIs" {
		t.Fatalf("resolved project ID = %q, want the opaque pin", got.ProjectID)
	}
	want := []document.RequirementBinding{{
		Reference: reference,
		Selector:  selector,
		Enabled:   true,
	}}
	if !reflect.DeepEqual(got.Requirements, want) {
		t.Fatalf("resolved requirements = %#v, want %#v", got.Requirements, want)
	}
	if !got.Enabled() {
		t.Fatal("resolved package lost its enabled declaration")
	}
}

// TestLockPackageForRequirementBinding pins the association order: a recorded
// binding identifies the pin only for the reference's own provider, while
// identity matching stays limited to the project ID and module identities.
func TestLockPackageForRequirementBinding(t *testing.T) {
	pkg := document.Package{
		Artifact: catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: "uXXizFIs",
			Modules: []catalog.DescriptorModule{{
				ID:        "ferritecore",
				Ecosystem: catalog.EcosystemFabric,
			}},
		},
		Requirements: []document.RequirementBinding{
			{Reference: "modrinth:ferrite-core", Selector: versionpkg.Any, Enabled: true},
			{Reference: "curseforge:other", Selector: versionpkg.Any, Enabled: true},
		},
	}
	tests := []struct {
		name string
		ref  string
		want bool
	}{
		{name: "recorded project slug", ref: "modrinth:ferrite-core", want: true},
		{name: "recorded binding needs the same provider", ref: "curseforge:other", want: false},
		{name: "unrecorded slug", ref: "modrinth:other", want: false},
		{name: "opaque project ID", ref: "modrinth:uXXizFIs", want: true},
		{name: "module ID", ref: "modrinth:ferritecore", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lockPackageFor(tt.ref, pkg); got != tt.want {
				t.Fatalf("lockPackageFor(%q) = %v, want %v", tt.ref, got, tt.want)
			}
		})
	}
}

func lockedMutationManifest(
	t *testing.T,
	reference string,
	selector versionpkg.Version,
) *document.Manifest {
	t.Helper()
	return &document.Manifest{
		SchemaVersion: document.ManifestSchemaVersion,
		Server: document.ManifestServer{
			Minecraft: versionpkg.Version("1.21.4"),
			Directory: ".",
			Core: document.ManifestCore{
				Product:        serverpkg.ProductPaper,
				ProductVersion: versionpkg.Version("1.21.4"),
			},
			Packages: map[catalog.Ecosystem]map[string]document.Requirement{
				catalog.EcosystemFabric: {
					reference: {Selector: selector},
				},
			},
		},
	}
}

func lockedMutationPackage(
	ecosystem catalog.Ecosystem,
	selector versionpkg.Version,
	enabled bool,
) document.Package {
	return document.Package{
		Environment: serverpkg.EnvironmentServer,
		Ecosystem:   ecosystem,
		Artifact: catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: "shared",
			ReleaseID: "shared-r1",
			FileID:    string(ecosystem) + "-f1",
			Version:   versionpkg.Version("1.0.0"),
			Filename:  "shared-" + string(ecosystem) + ".jar",
			Hashes:    map[string]string{"sha256": fmt.Sprintf("%064x", len(ecosystem))},
		},
		Requirements: []document.RequirementBinding{{
			Reference: "modrinth:shared",
			Selector:  selector,
			Enabled:   enabled,
		}},
	}
}

// TestMutateLockRetainsProviderThroughCrossEcosystemDependent covers a Quilt
// provider that replaced a Fabric dependency. Its transferred DependencyOf
// reference names the Fabric dependent, but the reference carries no ecosystem,
// so reachability must find that dependent across the shared Quilt module
// namespace. A locked removal of the provider's direct requirement keeps the
// provider while a dependent still needs it, and prunes it once none remains.
func TestMutateLockRetainsProviderThroughCrossEcosystemDependent(t *testing.T) {
	const (
		consumer = "consumer"
		provider = "provider"
	)
	consumerRef := catalog.Ref{Provider: catalog.ProviderModrinth, Name: consumer}
	tests := []struct {
		name              string
		dependent         catalog.Ecosystem
		dependency        catalog.Ecosystem
		dependentDeclared bool
		wantRetained      bool
	}{
		{
			name:              "quilt provider retained by fabric consumer",
			dependent:         catalog.EcosystemFabric,
			dependency:        catalog.EcosystemQuilt,
			dependentDeclared: true,
			wantRetained:      true,
		},
		{
			name:              "fabric dependency retained by quilt consumer",
			dependent:         catalog.EcosystemQuilt,
			dependency:        catalog.EcosystemFabric,
			dependentDeclared: true,
			wantRetained:      true,
		},
		{
			name:         "quilt provider pruned without its consumer",
			dependent:    catalog.EcosystemFabric,
			dependency:   catalog.EcosystemQuilt,
			wantRetained: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			final := lockedMutationManifest(t, "modrinth:"+consumer, versionpkg.Any)
			final.Server.Core.Product = serverpkg.ProductQuilt
			final.Server.Packages = map[catalog.Ecosystem]map[string]document.Requirement{}
			packages := []document.Package{
				crossEcosystemLockPackage(
					tt.dependency,
					provider,
					[]document.RequirementBinding{{
						Reference: "modrinth:" + provider,
						Selector:  versionpkg.Any,
						Enabled:   true,
					}},
					consumerRef,
				),
			}
			packages[0].Dependency = true
			if tt.dependentDeclared {
				final.Server.Packages[tt.dependent] = map[string]document.Requirement{
					"modrinth:" + consumer: {Selector: versionpkg.Any},
				}
				packages = append(packages, crossEcosystemLockPackage(
					tt.dependent,
					consumer,
					[]document.RequirementBinding{{
						Reference: "modrinth:" + consumer,
						Selector:  versionpkg.Any,
						Enabled:   true,
					}},
				))
			}
			lock := &document.Lock{
				SchemaVersion: document.LockSchemaVersion,
				Server: document.LockedServer{
					Directory:   "server",
					Minecraft:   versionpkg.Version("1.21.4"),
					Product:     serverpkg.ProductQuilt,
					CoreVersion: versionpkg.Version("0.29.1"),
					Artifact: catalog.Artifact{
						Provider:  catalog.ProviderQuilt,
						ProjectID: "quilt-loader",
						Version:   versionpkg.Version("0.29.1"),
						Filename:  "quilt-loader-0.29.1.jar",
						Hashes:    map[string]string{"sha256": fmt.Sprintf("%064x", 7)},
					},
					Bootstrapper: serverpkg.BootstrapMethodQuilt,
					Java:         document.LockedJava{Advisory: 21},
				},
				Packages: packages,
			}
			fingerprint, err := final.Fingerprint()
			if err != nil {
				t.Fatalf("manifest.Fingerprint() error = %v", err)
			}
			resolved, err := mutateLock(final, lock, fingerprint)
			if err != nil {
				t.Fatalf("mutateLock() error = %v", err)
			}

			var got *document.Package
			for i := range resolved.Packages {
				if resolved.Packages[i].ProjectID == provider {
					got = &resolved.Packages[i]
				}
			}
			if !tt.wantRetained {
				if got != nil {
					t.Fatalf("resolved retained %s %s; want it pruned", got.Ecosystem, provider)
				}
				return
			}
			if got == nil {
				t.Fatalf("resolved packages = %+v, want %s retained", resolved.Packages, provider)
			}
			if got.Ecosystem != tt.dependency {
				t.Fatalf("resolved %s ecosystem = %q, want %q", provider, got.Ecosystem, tt.dependency)
			}
			if len(got.Requirements) != 0 {
				t.Fatalf("resolved %s requirements = %#v, want none", provider, got.Requirements)
			}
			if !reflect.DeepEqual(got.DependencyOf, []catalog.Ref{consumerRef}) {
				t.Fatalf("resolved %s DependencyOf = %#v, want %#v", provider, got.DependencyOf, []catalog.Ref{consumerRef})
			}
			if !got.Dependency {
				t.Fatalf("resolved %s lost its dependency role", provider)
			}
		})
	}
}

func crossEcosystemLockPackage(
	ecosystem catalog.Ecosystem,
	projectID string,
	requirements []document.RequirementBinding,
	dependencyOf ...catalog.Ref,
) document.Package {
	return document.Package{
		Environment: serverpkg.EnvironmentServer,
		Ecosystem:   ecosystem,
		Artifact: catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: projectID,
			ReleaseID: projectID + "-r1",
			FileID:    projectID + "-f1",
			Version:   versionpkg.Version("1.0.0"),
			Filename:  projectID + ".jar",
			Hashes:    map[string]string{"sha256": fmt.Sprintf("%064x", len(projectID))},
		},
		Requirements: requirements,
		DependencyOf: dependencyOf,
	}
}
