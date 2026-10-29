package discovery

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

// vanillaIdentity names a plain Mojang runtime exactly as the vanilla
// detector projects it.
func vanillaIdentity(minecraft string) Identity {
	return Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      catalog.Name("minecraft"),
			Version:   versionpkg.Version(minecraft),
		},
	}
}

// hybridIdentity names a hybrid platform that bundles a mod loader, which is
// how hybrid detectors project their identity.
func hybridIdentity(
	name, minecraft, loader string,
	loaderEcosystem catalog.Ecosystem,
	loaderVersion string,
) Identity {
	return Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemUnspecified,
			Name:      catalog.Name(name),
			Version:   versionpkg.Version("1.0.1"),
		},
		Components: []catalog.VersionedRef{
			{
				Ecosystem: loaderEcosystem,
				Name:      catalog.Name(loader),
				Version:   versionpkg.Version(loaderVersion),
			},
			{
				Ecosystem: catalog.EcosystemMinecraft,
				Name:      catalog.Name("minecraft"),
				Version:   versionpkg.Version(minecraft),
			},
		},
	}
}

func managedInstallation(t *testing.T, identity Identity, entryPoints ...string) Installation {
	t.Helper()
	if len(entryPoints) == 0 {
		t.Fatalf("installation needs at least one entry point")
	}
	installation := NewInstallation(identity, entryPoints[0])
	for _, path := range entryPoints[1:] {
		installation.AddFile(path, RoleEntryPoint)
	}
	return installation
}

func markManaged(installation *Installation, path string) {
	for index := range installation.Files {
		if installation.Files[index].Path == path {
			installation.Files[index].Ownership = OwnershipManaged
		}
	}
}

func requireSelectionError(t *testing.T, err error, state SelectionState) *SelectionError {
	t.Helper()
	var selectionErr *SelectionError
	if !errors.As(err, &selectionErr) {
		t.Fatalf("selection error = %v, want *SelectionError", err)
	}
	if selectionErr.State != state {
		t.Fatalf("selection state = %q, want %q", selectionErr.State, state)
	}
	return selectionErr
}

func entryPointPaths(installation Installation) []string {
	paths := make([]string, 0)
	for file := range installation.EntryPoints() {
		paths = append(paths, file.Path)
	}
	return paths
}

// An explicit entry point decides between competing installations and records
// the request rather than a preference.
func TestSelectExplicitChoosesRequestedRuntime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fabricPath := filepath.Join(root, "fabric-server-launch.jar")
	vanillaPath := filepath.Join(root, "server.jar")
	result := Result{Installations: []Installation{
		NewInstallation(fabricIdentity("0.19.5", "1.21.1"), fabricPath),
		NewInstallation(vanillaIdentity("1.21.1"), vanillaPath),
	}}

	if err := result.Select(Request{EntryPoint: vanillaPath}); err != nil {
		t.Fatalf("Select explicit entry point: %v", err)
	}
	selection := result.Selection
	if selection.State != SelectionSelected || selection.Source != SourceExplicit {
		t.Fatalf(
			"selection = %q/%q, want selected/explicit",
			selection.State,
			selection.Source,
		)
	}
	if got := selection.Product(); got != server.ProductMinecraft {
		t.Fatalf("selected product = %q, want minecraft", got)
	}
	if selection.EntryPoint == nil || selection.EntryPoint.Path != vanillaPath {
		t.Fatalf("selected entry point = %+v, want %s", selection.EntryPoint, vanillaPath)
	}
	if got := selection.Ecosystem(); got != catalog.EcosystemMinecraft {
		t.Fatalf("selected ecosystem = %q, want minecraft", got)
	}
}

// An unrecognized explicit entry point fails closed and still reports every
// selectable installation so the caller can name the real choices.
func TestSelectExplicitRejectsUnrecognizedEntryPoint(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fabricPath := filepath.Join(root, "fabric-server-launch.jar")
	vanillaPath := filepath.Join(root, "server.jar")
	missingPath := filepath.Join(root, "absent.jar")
	result := Result{Installations: []Installation{
		NewInstallation(fabricIdentity("0.19.5", "1.21.1"), fabricPath),
		NewInstallation(vanillaIdentity("1.21.1"), vanillaPath),
	}}

	err := result.Select(Request{EntryPoint: missingPath})
	selectionErr := requireSelectionError(t, err, SelectionInvalid)
	if selectionErr.Path != missingPath {
		t.Fatalf("selection error path = %q, want %q", selectionErr.Path, missingPath)
	}
	if result.Selection.State != SelectionInvalid {
		t.Fatalf("selection state = %q, want invalid", result.Selection.State)
	}
	if result.Selected() != nil {
		t.Fatalf("invalid selection must not report a selected runtime")
	}
	var paths []string
	for _, installation := range selectionErr.Installations {
		paths = append(paths, entryPointPaths(installation)...)
	}
	slices.Sort(paths)
	want := []string{fabricPath, vanillaPath}
	if !slices.Equal(paths, want) {
		t.Fatalf("guidance entry points = %v, want %v", paths, want)
	}
}

// A readable declaration selects the installation whose observed identity
// agrees with it, and prefers the entry point the receipt manages.
func TestSelectDeclaredMatchesObservedIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	launcherPath := filepath.Join(root, "fabric-server-mc.1.21.1-loader.0.19.5-launcher.1.1.2.jar")
	shimPath := filepath.Join(root, "fabric-server-launch.jar")
	fabric := managedInstallation(t, fabricIdentity("0.19.5", "1.21.1"), launcherPath, shimPath)
	markManaged(&fabric, shimPath)
	result := Result{Installations: []Installation{
		fabric,
		NewInstallation(vanillaIdentity("1.21.1"), filepath.Join(root, "server.jar")),
	}}
	declaration := &Declaration{
		Product:     server.ProductFabric,
		Minecraft:   versionpkg.Version("1.21.1"),
		CoreVersion: versionpkg.Version("0.19.5"),
	}

	if err := result.Select(Request{Declared: declaration}); err != nil {
		t.Fatalf("Select declared installation: %v", err)
	}
	selection := result.Selection
	if selection.State != SelectionSelected || selection.Source != SourceDeclared {
		t.Fatalf(
			"selection = %q/%q, want selected/declared",
			selection.State,
			selection.Source,
		)
	}
	if selection.EntryPoint == nil || selection.EntryPoint.Path != shimPath {
		t.Fatalf(
			"selected entry point = %+v, want the managed shim %s",
			selection.EntryPoint,
			shimPath,
		)
	}
	if got := selection.MinecraftVersion().String(); got != "1.21.1" {
		t.Fatalf("selected Minecraft version = %q, want 1.21.1", got)
	}

	for finding := range result.Findings() {
		if finding.Kind != FindingAlternativeEntryPoint {
			continue
		}
		if finding.Path != launcherPath {
			t.Fatalf(
				"alternative entry point = %q, want the unmanaged launcher %s",
				finding.Path,
				launcherPath,
			)
		}
		return
	}
	t.Fatalf("no alternative-entry-point finding for the preserved launcher")
}

// Ownership labels never decide which installation a declaration selects: a
// modified or unmanaged entry point that still observes the declared identity
// remains the declared runtime.
func TestSelectDeclaredIgnoresOwnershipLabels(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	shimPath := filepath.Join(root, "fabric-server-launch.jar")
	fabric := NewInstallation(fabricIdentity("0.19.3", "26.2"), shimPath)
	fabric.Files[0].Ownership = OwnershipModified
	result := Result{Installations: []Installation{fabric}}
	declaration := &Declaration{
		Product:     server.ProductFabric,
		Minecraft:   versionpkg.Version("26.2"),
		CoreVersion: versionpkg.Version("0.19.3"),
	}

	if err := result.Select(Request{Declared: declaration}); err != nil {
		t.Fatalf("Select declared installation: %v", err)
	}
	if result.Selection.Source != SourceDeclared {
		t.Fatalf("selection source = %q, want declared", result.Selection.Source)
	}
	if result.Selection.EntryPoint == nil ||
		result.Selection.EntryPoint.Ownership != OwnershipModified {
		t.Fatalf(
			"selected entry point = %+v, want the modified shim unchanged",
			result.Selection.EntryPoint,
		)
	}
}

// A declaration matches an installation whose observed version is unknown:
// missing metadata neither confirms nor rejects the declared version.
func TestSelectDeclaredMatchesUnknownObservedVersion(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	shimPath := filepath.Join(root, "fabric-server-launch.jar")
	identity := Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemFabric,
			Name:      catalog.Name("fabric"),
			Version:   versionpkg.Unknown,
		},
		Components: []catalog.VersionedRef{{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      catalog.Name("minecraft"),
			Version:   versionpkg.Version("26.2"),
		}},
	}
	result := Result{Installations: []Installation{
		NewInstallation(identity, shimPath),
	}}
	declaration := &Declaration{
		Product:     server.ProductFabric,
		Minecraft:   versionpkg.Version("26.2"),
		CoreVersion: versionpkg.Version("0.19.3"),
	}

	if err := result.Select(Request{Declared: declaration}); err != nil {
		t.Fatalf("Select declared installation: %v", err)
	}
	if result.Selection.EntryPoint == nil || result.Selection.EntryPoint.Path != shimPath {
		t.Fatalf("selected entry point = %+v, want %s", result.Selection.EntryPoint, shimPath)
	}
}

// A declared installation that is gone, or that remains only under a
// different identity, reports a missing declaration instead of adopting a
// substitute.
func TestSelectDeclaredReportsMissingInstallation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	tests := []struct {
		name        string
		observed    Identity
		declaration Declaration
	}{
		{
			name:     "different product remains",
			observed: vanillaIdentity("26.2"),
			declaration: Declaration{
				Product:     server.ProductFabric,
				Minecraft:   versionpkg.Version("26.2"),
				CoreVersion: versionpkg.Version("0.19.3"),
			},
		},
		{
			name:     "different loader version remains",
			observed: fabricIdentity("0.16.9", "1.21.4"),
			declaration: Declaration{
				Product:     server.ProductFabric,
				Minecraft:   versionpkg.Version("26.2"),
				CoreVersion: versionpkg.Version("0.19.3"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(root, "server.jar")
			result := Result{Installations: []Installation{
				NewInstallation(tt.observed, path),
			}}
			err := result.Select(Request{Declared: &tt.declaration})
			requireSelectionError(t, err, SelectionMissingDeclared)
			if result.Selection.State != SelectionMissingDeclared {
				t.Fatalf(
					"selection state = %q, want missing_declared",
					result.Selection.State,
				)
			}
			found := false
			for finding := range result.Findings() {
				if finding.Kind == FindingMissingDeclaredInstallation {
					found = true
				}
			}
			if !found {
				t.Fatalf("no missing-declared-installation finding")
			}
		})
	}
}

// Live status resolves competing runtimes by the fixed preference order and
// reports the automatic choice together with its competitors.
func TestSelectPreferenceOrderChoosesDeterministically(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hybridPath := filepath.Join(root, "arclight.jar")
	loaderPath := filepath.Join(root, "fabric-server-launch.jar")
	vanillaPath := filepath.Join(root, "server.jar")
	result := Result{Installations: []Installation{
		NewInstallation(vanillaIdentity("1.21.1"), vanillaPath),
		NewInstallation(fabricIdentity("0.16.9", "1.21.1"), loaderPath),
		NewInstallation(
			hybridIdentity("catserver", "1.21.1", "fabric-loader", catalog.EcosystemFabric, "0.16.9"),
			hybridPath,
		),
	}}

	if err := result.Select(Request{Policy: PolicyPreferModified}); err != nil {
		t.Fatalf("Select preferred installation: %v", err)
	}
	selection := result.Selection
	if selection.Source != SourcePreferred {
		t.Fatalf("selection source = %q, want preferred", selection.Source)
	}
	if selection.EntryPoint == nil || selection.EntryPoint.Path != hybridPath {
		t.Fatalf(
			"selected entry point = %+v, want the hybrid %s",
			selection.EntryPoint,
			hybridPath,
		)
	}

	var automatic *Finding
	for finding := range result.Findings() {
		if finding.Kind != FindingAutomaticSelection {
			continue
		}
		copied := finding
		automatic = &copied
	}
	if automatic == nil {
		t.Fatalf("no automatic-selection finding")
	}
	if automatic.Path != hybridPath {
		t.Fatalf("automatic selection path = %q, want %q", automatic.Path, hybridPath)
	}
	wantCompeting := []string{loaderPath, vanillaPath}
	if !slices.Equal(automatic.Paths, wantCompeting) {
		t.Fatalf("competing entry points = %v, want %v", automatic.Paths, wantCompeting)
	}
}

// The loaders themselves rank by identity, not by discovery order, so the
// same directory always reports the same runtime.
func TestSelectPreferenceBreaksLoaderTiesDeterministically(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	newerPath := filepath.Join(root, "fabric-server-mc.26.2-loader.0.19.3-launcher.1.1.2.jar")
	olderPath := filepath.Join(root, "fabric-server-mc.1.21.4-loader.0.16.9-launcher.1.0.1.jar")
	newer := NewInstallation(fabricIdentity("0.19.3", "26.2"), newerPath)
	older := NewInstallation(fabricIdentity("0.16.9", "1.21.4"), olderPath)

	for _, installations := range [][]Installation{
		{newer, older},
		{older, newer},
	} {
		result := Result{Installations: slices.Clone(installations)}
		if err := result.Select(Request{Policy: PolicyPreferModified}); err != nil {
			t.Fatalf("Select preferred installation: %v", err)
		}
		if result.Selection.EntryPoint == nil ||
			result.Selection.EntryPoint.Path != olderPath {
			t.Fatalf(
				"selected entry point = %+v, want the lexical loader identity %s",
				result.Selection.EntryPoint,
				olderPath,
			)
		}
	}
}

// A receipt-managed entry point outranks the lexical first entry point of the
// same installation, while an unmanaged installation still prefers a managed
// sibling.
func TestSelectPrefersManagedEntryPointWithinTier(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	unmanagedPath := filepath.Join(root, "a-fabric-launcher.jar")
	managedPath := filepath.Join(root, "b-fabric-launcher.jar")
	unmanaged := NewInstallation(fabricIdentity("0.16.9", "1.21.4"), unmanagedPath)
	managed := NewInstallation(fabricIdentity("0.19.3", "26.2"), managedPath)
	markManaged(&managed, managedPath)
	result := Result{Installations: []Installation{unmanaged, managed}}

	if err := result.Select(Request{Policy: PolicyPreferModified}); err != nil {
		t.Fatalf("Select preferred installation: %v", err)
	}
	if result.Selection.EntryPoint == nil ||
		result.Selection.EntryPoint.Path != managedPath {
		t.Fatalf(
			"selected entry point = %+v, want the managed %s",
			result.Selection.EntryPoint,
			managedPath,
		)
	}
}

// Strict selection never guesses: competing runtimes stay an error whose
// installations name every candidate, and non-runtime content is skipped.
func TestSelectStrictReportsAmbiguity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fabricPath := filepath.Join(root, "fabric-server-launch.jar")
	vanillaPath := filepath.Join(root, "server.jar")
	unreadablePath := filepath.Join(root, "corrupt.jar")
	invalid := NewInstallation(Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      catalog.Name("minecraft"),
			Version:   versionpkg.Version("1.21.1"),
		},
		Components: []catalog.VersionedRef{
			{
				Ecosystem: catalog.EcosystemMinecraft,
				Name:      catalog.Name("minecraft"),
				Version:   versionpkg.Version("1.21.4"),
			},
		},
	}, unreadablePath)
	result := Result{Installations: []Installation{
		NewInstallation(fabricIdentity("0.19.5", "1.21.1"), fabricPath),
		NewInstallation(vanillaIdentity("1.21.1"), vanillaPath),
		invalid,
	}}

	err := result.Select(Request{Policy: PolicyStrict})
	selectionErr := requireSelectionError(t, err, SelectionAmbiguous)
	if len(selectionErr.Installations) != 2 {
		t.Fatalf(
			"ambiguous candidates = %d, want the 2 selectable installations",
			len(selectionErr.Installations),
		)
	}
	var paths []string
	for finding := range result.Findings() {
		if finding.Kind == FindingAmbiguousInstallations {
			paths = finding.Paths
		}
	}
	want := []string{fabricPath, vanillaPath}
	if !slices.Equal(paths, want) {
		t.Fatalf("ambiguous finding paths = %v, want %v", paths, want)
	}
}

// An empty directory and a directory with no launchable installation are
// distinct outcomes: only the latter can still name files to inspect.
func TestSelectReportsUnresolvedLayouts(t *testing.T) {
	t.Parallel()

	var empty Result
	requireSelectionError(t, empty.Select(Request{}), SelectionNone)
	if empty.Selection.State != SelectionNone {
		t.Fatalf("empty selection state = %q, want none", empty.Selection.State)
	}

	root := t.TempDir()
	invalid := NewInstallation(Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      catalog.Name("minecraft"),
			Version:   versionpkg.Version("1.21.1"),
		},
		Components: []catalog.VersionedRef{
			{
				Ecosystem: catalog.EcosystemMinecraft,
				Name:      catalog.Name("minecraft"),
				Version:   versionpkg.Version("1.21.4"),
			},
		},
	}, filepath.Join(root, "server.jar"))
	layout := Result{Installations: []Installation{invalid}}
	selectionErr := requireSelectionError(
		t,
		layout.Select(Request{}),
		SelectionInvalid,
	)
	if len(selectionErr.Installations) != 1 {
		t.Fatalf(
			"unresolved layout candidates = %d, want the observed installation",
			len(selectionErr.Installations),
		)
	}
}
