package install_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

// TestLockedMutationBindsEachDeclarationToLocation covers a
// reference declared for two ecosystems. Each locked package must receive its
// own declaration instead of the last one applied to every package that names
// the same reference.
func TestLockedMutationBindsEachDeclarationToLocation(t *testing.T) {
	root := t.TempDir()
	initial := lockedMutationManifest(t, "modrinth:shared", versionpkg.Any)
	if err := document.ManifestFile(root).Write(initial); err != nil {
		t.Fatalf("manifest.Write() error = %v", err)
	}
	fingerprint, err := initial.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: fingerprint,
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
	if err := document.LockFile(root).Write(lock); err != nil {
		t.Fatalf("lockfile.Write() error = %v", err)
	}

	fabricSelector := versionpkg.Version("1.0.0")
	disabled := false
	final := lockedMutationManifest(t, "modrinth:shared", fabricSelector)
	final.Server.Packages[catalog.EcosystemPaper] = map[string]document.Requirement{
		"modrinth:shared": {Selector: versionpkg.Any, Enabled: &disabled},
	}
	if err := document.ManifestFile(root).Write(final); err != nil {
		t.Fatalf("manifest.Write() error = %v", err)
	}

	onDiskManifest, err := document.ManifestFile(root).Read()
	if err != nil {
		t.Fatalf("manifest.Read() error = %v", err)
	}
	onDiskLock, err := document.LockFile(root).Read()
	if err != nil {
		t.Fatalf("lockfile.Read() error = %v", err)
	}
	plan, err := install.NewService(nil, nil, nil, nil, nil, "").PlanInstall(
		t.Context(),
		root,
		onDiskManifest,
		onDiskLock,
		install.Options{Locked: true, RequirementChange: true, Offline: true},
	)
	if err != nil {
		t.Fatalf("PlanInstall() error = %v", err)
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
	if len(plan.Resolved.Packages) != len(want) {
		t.Fatalf("resolved packages = %d, want %d: %+v", len(plan.Resolved.Packages), len(want), plan.Resolved.Packages)
	}
	for _, pkg := range plan.Resolved.Packages {
		wantBinding, ok := want[pkg.Ecosystem]
		if !ok {
			t.Fatalf("resolved unexpected ecosystem %q", pkg.Ecosystem)
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
