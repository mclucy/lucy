package cli

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

// selectedWorkspace builds one observation whose discovery result already
// selects the first installation and entry point.
func selectedWorkspace(
	root string,
	identity discovery.Identity,
	entryPoint string,
) *workspace.Workspace {
	observed := &workspace.Workspace{ServerDir: root}
	observed.Discovery.Installations = []discovery.Installation{
		cliTestInstallation(identity, entryPoint),
	}
	observed.Discovery.Selection = discovery.Selection{
		State:        discovery.SelectionSelected,
		Source:       discovery.SourceDeclared,
		Installation: &observed.Discovery.Installations[0],
		EntryPoint:   &observed.Discovery.Installations[0].Files[0],
	}
	return observed
}

func documentComponent(document statusDocument, ecosystem, name string) string {
	for _, component := range document.Server.Components {
		if component.Ecosystem == ecosystem && component.Name == name {
			return component.Version
		}
	}
	return ""
}

// The live status document projects the selected runtime, its components, and
// the discovered content packages; runtime components and server products are
// never reported as content packages.
func TestNewLiveStatusDocumentProjectsObservation(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "srv", "server")
	entryPoint := filepath.Join(root, "fabric-server-launch.jar")
	observed := selectedWorkspace(
		root,
		cliTestFabricIdentity("0.19.5", "1.21.1"),
		entryPoint,
	)
	observed.Discovery.Installations[0].Files[0].Ownership = discovery.OwnershipManaged
	observed.Packages = []workspace.DiscoveredPackage{
		{
			ID:   cliTestRef(catalog.EcosystemFabric, "fabric-loader", "0.19.5"),
			Path: filepath.Join(root, "mods", "fabric-loader.jar"),
		},
		{
			ID:   cliTestRef(catalog.EcosystemMinecraft, "minecraft", "1.21.1"),
			Path: filepath.Join(root, "server.jar"),
		},
		{
			ID:   cliTestRef(catalog.EcosystemFabric, "sodium", "0.6.0"),
			Path: filepath.Join(root, "mods", "sodium.jar"),
		},
	}

	document := newLiveStatusDocument(observed, root)
	if document.Schema != statusDocumentSchema ||
		document.Kind != statusDocumentKind ||
		document.Source != statusSourceLive ||
		document.Root != root {
		t.Fatalf("document envelope = %+v, want live status for %s", document, root)
	}
	if document.Server == nil {
		t.Fatalf("live status reported no server for a selected runtime")
	}
	server := document.Server
	if server.Product.Name != "fabric" ||
		server.Product.Version != "0.19.5" ||
		server.Product.Ecosystem != catalog.EcosystemFabric.String() {
		t.Fatalf("server product = %+v, want fabric/0.19.5", server.Product)
	}
	if server.Minecraft != "1.21.1" {
		t.Fatalf("server Minecraft = %q, want 1.21.1", server.Minecraft)
	}
	if server.EntryPoint != "fabric-server-launch.jar" {
		t.Fatalf("entry point = %q, want the workspace-relative launcher", server.EntryPoint)
	}
	if server.Activity != statusActivityStopped {
		t.Fatalf("activity = %q, want stopped", server.Activity)
	}
	if got := documentComponent(document, "minecraft", "minecraft"); got != "1.21.1" {
		t.Fatalf("minecraft component = %q, want 1.21.1", got)
	}
	if got := documentComponent(document, "fabric", "fabric-loader"); got != "0.19.5" {
		t.Fatalf("loader component = %q, want 0.19.5", got)
	}
	if len(server.CompatibleEcosystems) != 1 ||
		server.CompatibleEcosystems[0].Ecosystem != catalog.EcosystemFabric.String() ||
		server.CompatibleEcosystems[0].Compatibility != string(workspace.CompatFull) {
		t.Fatalf(
			"compatible ecosystems = %+v, want full fabric support",
			server.CompatibleEcosystems,
		)
	}
	if len(server.PackageDirectories) != 1 || server.PackageDirectories[0] != "mods" {
		t.Fatalf("package directories = %v, want [mods]", server.PackageDirectories)
	}
	if len(document.Packages) != 1 {
		t.Fatalf(
			"packages = %+v, want only the content package",
			document.Packages,
		)
	}
	pkg := document.Packages[0]
	if pkg.Name != "sodium" ||
		pkg.Version != "0.6.0" ||
		pkg.Ecosystem != catalog.EcosystemFabric.String() ||
		pkg.Environment != "server" ||
		pkg.Path != "mods/sodium.jar" {
		t.Fatalf("package = %+v, want mods/sodium.jar as a server package", pkg)
	}
}

// An unresolved layout still produces a document, but without a server; only
// an unusable selection fails the command.
func TestNewLiveStatusDocumentWithoutSelectedRuntime(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "srv", "server")
	observed := &workspace.Workspace{ServerDir: root}
	observed.Discovery.Installations = []discovery.Installation{
		cliTestInstallation(
			cliTestFabricIdentity("0.19.5", "1.21.1"),
			filepath.Join(root, "fabric-server-launch.jar"),
		),
		cliTestInstallation(
			cliTestVanillaIdentity("1.21.1"),
			filepath.Join(root, "server.jar"),
		),
	}
	if err := observed.Select(discovery.Request{Policy: discovery.PolicyStrict}); err == nil {
		t.Fatalf("strict selection accepted competing runtimes")
	}

	document := newLiveStatusDocument(observed, root)
	if document.Server != nil {
		t.Fatalf("unresolved layout reported a server: %+v", document.Server)
	}
	if len(document.Packages) != 0 {
		t.Fatalf("unresolved layout reported packages: %+v", document.Packages)
	}
	if failure := statusSelectionFailure(observed); failure != nil {
		t.Fatalf("ambiguous layout failed the command: %v", failure)
	}

	if err := observed.Select(discovery.Request{EntryPoint: filepath.Join(root, "notes.txt")}); err == nil {
		t.Fatalf("invalid entry point selected a runtime")
	}
	failure := statusSelectionFailure(observed)
	var detail *report.Detail
	if !errors.As(failure, &detail) {
		t.Fatalf("failure = %T, want *report.Detail", failure)
	}
	if detail.Code != statusErrorSelectionFailed {
		t.Fatalf("failure code = %q, want %q", detail.Code, statusErrorSelectionFailed)
	}
}
