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
