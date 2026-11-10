package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/server"
)

const (
	quiltTestLoaderVersion    = "0.27.1"
	quiltTestMinecraftVersion = "1.21.1"
	quiltTestLauncherName     = "quilt-server-launch.jar"
	quiltTestPropertiesName   = "quilt-server-launcher.properties"
	quiltTestLauncherMain     = "org.quiltmc.loader.impl.launch.server.QuiltServerLauncher"
	quiltTestKnotMainClass    = "org.quiltmc.loader.impl.launch.knot.KnotServer"
)

// quiltTestServerFixture is one on-disk Quilt server layout. The launcher is
// the 478-byte manifest-only JAR the Quilt installer writes; libraries use the
// Maven layout from the Quilt Meta server profile.
type quiltTestServerFixture struct {
	root           string
	launcherPath   string
	serverJarPath  string
	propertiesPath string
	loaderJarPath  string
}

type quiltTestFixtureOptions struct {
	loader                   string
	minecraft                string
	embeddedLaunchProperties bool
	withServerJar            bool
}

// quiltTestClassPath returns the profile libraries the launcher manifest wires
// onto its class path, relative to the server directory.
func quiltTestClassPath(loader, minecraft string) []string {
	return []string{
		"libraries/org/quiltmc/quilt-loader/" + loader +
			"/quilt-loader-" + loader + ".jar",
		"libraries/org/quiltmc/hashed/" + minecraft +
			"/hashed-" + minecraft + ".jar",
		"libraries/net/fabricmc/intermediary/" + minecraft +
			"/intermediary-" + minecraft + ".jar",
		"libraries/net/fabricmc/sponge-mixin/0.15.4+mixin.0.8.7" +
			"/sponge-mixin-0.15.4+mixin.0.8.7.jar",
		"libraries/org/quiltmc/quilt-json5/1.0.4+final" +
			"/quilt-json5-1.0.4+final.jar",
		"libraries/org/quiltmc/quilt-config/1.3.1" +
			"/quilt-config-1.3.1.jar",
	}
}

// quiltTestFoldManifestLine wraps a manifest attribute the way
// java.util.jar.Manifest does: continuation lines start with one space and
// carry the value without inserting separators.
func quiltTestFoldManifestLine(line string) string {
	const limit = 70
	var builder strings.Builder
	for len(line) > limit {
		builder.WriteString(line[:limit])
		builder.WriteString("\n ")
		line = line[limit:]
	}
	builder.WriteString(line)
	return builder.String()
}

// quiltTestLauncherFiles builds the real Quilt launcher layout. The official
// installer writes only the manifest, while Lucy's generated launcher also
// embeds the KnotServer launch properties.
func quiltTestLauncherFiles(
	loader string,
	minecraft string,
	embeddedLaunchProperties bool,
) map[string]string {
	return quiltTestLauncherManifest(
		quiltTestClassPath(loader, minecraft),
		embeddedLaunchProperties,
	)
}

func quiltTestLauncherManifest(
	classPath []string,
	embeddedLaunchProperties bool,
) map[string]string {
	manifest := "Manifest-Version: 1.0\nMain-Class: " + quiltTestLauncherMain + "\n"
	if len(classPath) > 0 {
		manifest += quiltTestFoldManifestLine(
			"Class-Path: "+strings.Join(classPath, " "),
		) + "\n"
	}
	files := map[string]string{"META-INF/MANIFEST.MF": manifest}
	if embeddedLaunchProperties {
		files["quilt-server-launch.properties"] =
			"launch.mainClass=" + quiltTestKnotMainClass + "\n"
	}
	return files
}

func quiltTestVanillaVersionJSON(minecraft string) string {
	return `{"id":"` + minecraft + `","name":"` + minecraft + `"}`
}

func quiltTestVanillaFiles(minecraft string) map[string]string {
	return map[string]string{
		"version.json": quiltTestVanillaVersionJSON(minecraft),
	}
}

// writeQuiltTestServer writes one Quilt server directory without any first-run
// output. Every class path entry exists so role attribution is observable.
func writeQuiltTestServer(
	t *testing.T,
	options quiltTestFixtureOptions,
) quiltTestServerFixture {
	t.Helper()

	loader := options.loader
	if loader == "" {
		loader = quiltTestLoaderVersion
	}
	minecraft := options.minecraft
	if minecraft == "" {
		minecraft = quiltTestMinecraftVersion
	}
	root := t.TempDir()

	fixture := quiltTestServerFixture{
		root:           root,
		launcherPath:   filepath.Join(root, quiltTestLauncherName),
		serverJarPath:  filepath.Join(root, "server.jar"),
		propertiesPath: filepath.Join(root, quiltTestPropertiesName),
	}

	writeZipFile(
		t,
		fixture.launcherPath,
		quiltTestLauncherFiles(loader, minecraft, options.embeddedLaunchProperties),
	)
	for _, entry := range quiltTestClassPath(loader, minecraft) {
		path := filepath.Join(root, filepath.FromSlash(entry))
		writeZipFile(
			t,
			path,
			map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\n"},
		)
		if strings.Contains(entry, "/quilt-loader/") {
			fixture.loaderJarPath = path
		}
	}
	writeFile(
		t,
		fixture.propertiesPath,
		[]byte("serverJar=server.jar\n"),
	)
	if options.withServerJar {
		writeZipFile(t, fixture.serverJarPath, quiltTestVanillaFiles(minecraft))
	}
	return fixture
}

// quiltTestInstallation returns the single installation carrying the Quilt
// product, and fails when the observation has any other shape.
func quiltTestInstallation(t *testing.T, result Result) Installation {
	t.Helper()

	var found []Installation
	for _, installation := range result.Installations {
		if installation.Identity.Product() == server.ProductQuilt {
			found = append(found, installation)
		}
	}
	if len(found) != 1 {
		t.Fatalf(
			"Quilt installations = %d, want exactly one (all products: %v)",
			len(found),
			resultProductNames(result),
		)
	}
	return found[0]
}

func resultProductNames(result Result) []string {
	names := make([]string, 0, len(result.Installations))
	for _, installation := range result.Installations {
		names = append(names, installation.Identity.Product().String())
	}
	slices.Sort(names)
	return names
}

func quiltTestHasPath(installation Installation, path string) bool {
	for _, file := range installation.Files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func quiltTestFindFile(installation Installation, path string) *File {
	for index := range installation.Files {
		if installation.Files[index].Path == path {
			return &installation.Files[index]
		}
	}
	return nil
}

func quiltTestAssertManaged(
	t *testing.T,
	installation Installation,
	path string,
) {
	t.Helper()

	file := quiltTestFindFile(installation, path)
	if file == nil {
		t.Fatalf("expected %s claimed by Quilt installation", path)
	}
	if file.Ownership != OwnershipManaged {
		t.Fatalf(
			"ownership of %s = %q, want %q",
			path,
			file.Ownership,
			OwnershipManaged,
		)
	}
}

func quiltTestWriteReceipt(t *testing.T, root string, paths ...string) {
	t.Helper()

	recorded := receipt.New()
	for _, path := range paths {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("relativize receipt path %s: %v", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read receipt target %s: %v", path, err)
		}
		sum := sha256.Sum256(data)
		recorded.Entries = append(recorded.Entries, receipt.Entry{
			Path:   filepath.ToSlash(relative),
			SHA256: hex.EncodeToString(sum[:]),
			Mode:   0o644,
			Origin: receipt.OriginBootstrap,
		})
	}
	data, err := recorded.Snapshot()
	if err != nil {
		t.Fatalf("snapshot receipt: %v", err)
	}
	writeFile(
		t,
		filepath.Join(root, filepath.FromSlash(receipt.Filename)),
		data,
	)
}

// A real installed Quilt layout is one coherent server installation: the
// launcher is the entry point, the Quilt loader and profile libraries are
// libraries, the queued vanilla jar is the payload, and the first-run
// properties sidecar is configuration.
func TestDiscoverQuiltServerRecognizesInstalledLayout(t *testing.T) {
	t.Parallel()

	fixture := writeQuiltTestServer(
		t,
		quiltTestFixtureOptions{
			embeddedLaunchProperties: true,
			withServerJar:            true,
		},
	)

	result := Discover(fixture.root)
	installation := quiltTestInstallation(t, result)
	if got := installation.Identity.Primary.Ecosystem; got != catalog.EcosystemQuilt {
		t.Fatalf("primary ecosystem = %q, want %q", got, catalog.EcosystemQuilt)
	}
	if got := installation.Identity.Ecosystem(); got != catalog.EcosystemQuilt {
		t.Fatalf("identity ecosystem = %q, want %q", got, catalog.EcosystemQuilt)
	}
	if got := installation.Identity.Primary.Version.String(); got != quiltTestLoaderVersion {
		t.Fatalf("loader version = %q, want %q", got, quiltTestLoaderVersion)
	}
	if got := installation.Identity.MinecraftVersion().String(); got != quiltTestMinecraftVersion {
		t.Fatalf(
			"Minecraft version = %q, want %q",
			got,
			quiltTestMinecraftVersion,
		)
	}
	if got := entryPointPaths(installation); !slices.Equal(
		got,
		[]string{fixture.launcherPath},
	) {
		t.Fatalf("entry points = %v, want [%s]", got, fixture.launcherPath)
	}

	roles := []struct {
		path string
		want FileRole
	}{
		{fixture.launcherPath, RoleEntryPoint},
		{fixture.serverJarPath, RolePayload},
		{fixture.propertiesPath, RoleConfiguration},
		{fixture.loaderJarPath, RoleLibrary},
	}
	for _, role := range roles {
		if got := installationFileRole(installation, role.path); got != role.want {
			t.Fatalf(
				"role of %s = %q, want %q",
				role.path,
				got,
				role.want,
			)
		}
	}
}

// The official Quilt installer writes a manifest-only launch jar and no
// embedded launch properties. Recognition must not depend on Lucy's own
// generated launcher shape.
func TestDiscoverQuiltRecognizesInstallerLauncher(t *testing.T) {
	t.Parallel()

	fixture := writeQuiltTestServer(
		t,
		quiltTestFixtureOptions{
			embeddedLaunchProperties: false,
			withServerJar:            true,
		},
	)

	installation := quiltTestInstallation(t, Discover(fixture.root))
	if got := installation.Identity.Primary.Version.String(); got != quiltTestLoaderVersion {
		t.Fatalf("loader version = %q, want %q", got, quiltTestLoaderVersion)
	}
	if got := installation.Identity.MinecraftVersion().String(); got != quiltTestMinecraftVersion {
		t.Fatalf(
			"Minecraft version = %q, want %q",
			got,
			quiltTestMinecraftVersion,
		)
	}
	if got := entryPointPaths(installation); !slices.Equal(
		got,
		[]string{fixture.launcherPath},
	) {
		t.Fatalf("entry points = %v, want [%s]", got, fixture.launcherPath)
	}
}

// A launcher that proves only half its identity is not a Quilt installation.
// The recognizer never invents a loader or game version from the filename.
func TestDiscoverQuiltRequiresCompleteManifestIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		classPath []string
	}{
		{name: "no class path"},
		{
			name: "loader only",
			classPath: []string{
				"libraries/org/quiltmc/quilt-loader/" +
					quiltTestLoaderVersion + "/quilt-loader-" +
					quiltTestLoaderVersion + ".jar",
			},
		},
		{
			name: "game only",
			classPath: []string{
				"libraries/net/fabricmc/intermediary/" +
					quiltTestMinecraftVersion + "/intermediary-" +
					quiltTestMinecraftVersion + ".jar",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeZipFile(
				t,
				filepath.Join(root, quiltTestLauncherName),
				quiltTestLauncherManifest(test.classPath, true),
			)

			result := Discover(root)
			for _, installation := range result.Installations {
				if installation.Identity.Product() == server.ProductQuilt {
					t.Fatalf(
						"partial manifest %q observed as Quilt: %+v",
						test.name,
						installation.Identity,
					)
				}
			}
		})
	}
}

// A launcher whose embedded launch properties select another loader is not a
// Quilt installation, even when its manifest names the Quilt loader and game.
func TestDiscoverQuiltRejectsForeignLaunchProperties(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeZipFile(
		t,
		filepath.Join(root, quiltTestLauncherName),
		map[string]string{
			"META-INF/MANIFEST.MF": quiltTestLauncherManifest(
				quiltTestClassPath(
					quiltTestLoaderVersion,
					quiltTestMinecraftVersion,
				),
				false,
			)["META-INF/MANIFEST.MF"],
			"quilt-server-launch.properties": "launch.mainClass=" +
				"net.fabricmc.loader.impl.launch.knot.KnotServer\n",
		},
	)

	result := Discover(root)
	for _, installation := range result.Installations {
		if installation.Identity.Product() == server.ProductQuilt {
			t.Fatalf(
				"foreign launch properties observed as Quilt: %+v",
				installation.Identity,
			)
		}
	}
}

// Quilt and Fabric launchers differ by main class, not by filename. A Fabric
// launcher is never observed as Quilt, even when its file is named like a
// Quilt launcher.
func TestDiscoverQuiltDoesNotClaimFabricLauncher(t *testing.T) {
	t.Parallel()

	fabricFiles := fabricMetaLauncherFiles("0.19.5", "1.21.1")
	tests := []struct {
		name     string
		baseName string
	}{
		{name: "fabric filename", baseName: "fabric-server-launch.jar"},
		{name: "quilt filename", baseName: quiltTestLauncherName},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeRootJarIn(t, root, test.baseName, fabricFiles)

			result := Discover(root)
			for _, installation := range result.Installations {
				if installation.Identity.Product() == server.ProductQuilt {
					t.Fatalf(
						"Fabric launcher %s observed as Quilt: %+v",
						test.baseName,
						installation.Identity,
					)
				}
			}
			if len(result.Installations) != 1 ||
				result.Installations[0].Identity.Product() != server.ProductFabric {
				t.Fatalf(
					"installations = %v, want one Fabric installation",
					resultProductNames(result),
				)
			}
		})
	}
}

// A plain vanilla server is never promoted to Quilt.
func TestDiscoverQuiltDoesNotClaimVanillaServer(t *testing.T) {
	t.Parallel()

	jarPath := writeRootJar(
		t,
		"server.jar",
		quiltTestVanillaFiles(quiltTestMinecraftVersion),
	)

	result := Discover(filepath.Dir(jarPath))
	for _, installation := range result.Installations {
		if installation.Identity.Product() == server.ProductQuilt {
			t.Fatalf(
				"vanilla server observed as Quilt: %+v",
				installation.Identity,
			)
		}
	}
}

// The launcher's payload claim is content-bound. A server jar that does not
// carry the observed game version is not the Quilt installation's payload.
func TestDiscoverQuiltDoesNotClaimMismatchedServerPayload(t *testing.T) {
	t.Parallel()

	fixture := writeQuiltTestServer(
		t,
		quiltTestFixtureOptions{
			embeddedLaunchProperties: true,
			withServerJar:            false,
		},
	)
	staleMinecraft := "1.20.1"
	writeZipFile(
		t,
		fixture.serverJarPath,
		quiltTestVanillaFiles(staleMinecraft),
	)

	installation := quiltTestInstallation(t, Discover(fixture.root))
	if quiltTestHasPath(installation, fixture.serverJarPath) {
		t.Fatalf(
			"mismatched server payload %s claimed by Quilt installation",
			fixture.serverJarPath,
		)
	}
}

// First-run output belongs to the Quilt installation only when its content
// names the observed game version. A stale versions/ jar from another release
// and an unrelated transform-cache entry stay unclaimed.
func TestDiscoverQuiltClaimsFirstRunPayloadOnlyOnIdentityMatch(t *testing.T) {
	t.Parallel()

	fixture := writeQuiltTestServer(
		t,
		quiltTestFixtureOptions{
			embeddedLaunchProperties: true,
			withServerJar:            true,
		},
	)
	matching := filepath.Join(
		fixture.root,
		"versions",
		quiltTestMinecraftVersion,
		"server-"+quiltTestMinecraftVersion+".jar",
	)
	writeZipFile(
		t,
		matching,
		quiltTestVanillaFiles(quiltTestMinecraftVersion),
	)
	staleMinecraft := "1.20.1"
	stale := filepath.Join(
		fixture.root,
		"versions",
		staleMinecraft,
		"server-"+staleMinecraft+".jar",
	)
	writeZipFile(t, stale, quiltTestVanillaFiles(staleMinecraft))
	cacheEntry := filepath.Join(
		fixture.root,
		".cache",
		"quilt_loader",
		"transformed",
		"stale.jar",
	)
	writeZipFile(
		t,
		cacheEntry,
		quiltTestVanillaFiles(staleMinecraft),
	)

	installation := quiltTestInstallation(t, Discover(fixture.root))
	matchingFile := quiltTestFindFile(installation, matching)
	if matchingFile == nil {
		t.Fatalf(
			"matching first-run payload %s not claimed",
			matching,
		)
	}
	if matchingFile.Role != RolePayload {
		t.Fatalf(
			"role of matching first-run payload = %q, want %q",
			matchingFile.Role,
			RolePayload,
		)
	}
	for _, unrelated := range []string{stale, cacheEntry} {
		if quiltTestHasPath(installation, unrelated) {
			t.Fatalf(
				"unrelated first-run file %s claimed by Quilt installation",
				unrelated,
			)
		}
	}
}

// Ownership follows claims: receipt-listed files that the recognizer proved
// part of the Quilt installation are managed, while receipt-listed first-run
// files with unrelated content are never claimed and therefore never owned.
// When a claimed first-run payload changes, a fresh scan stops claiming it
// instead of inheriting ownership from the path alone.
func TestObserveOwnershipQuiltFirstRunClaimsAreIdentityBound(t *testing.T) {
	t.Parallel()

	fixture := writeQuiltTestServer(
		t,
		quiltTestFixtureOptions{
			embeddedLaunchProperties: true,
			withServerJar:            true,
		},
	)
	matching := filepath.Join(
		fixture.root,
		"versions",
		quiltTestMinecraftVersion,
		"server-"+quiltTestMinecraftVersion+".jar",
	)
	writeZipFile(
		t,
		matching,
		quiltTestVanillaFiles(quiltTestMinecraftVersion),
	)
	stale := filepath.Join(
		fixture.root,
		"versions",
		"1.20.1",
		"server-1.20.1.jar",
	)
	writeZipFile(t, stale, quiltTestVanillaFiles("1.20.1"))
	cacheEntry := filepath.Join(
		fixture.root,
		".cache",
		"quilt_loader",
		"transformed",
		"stale.jar",
	)
	writeZipFile(t, cacheEntry, quiltTestVanillaFiles("1.20.1"))

	quiltTestWriteReceipt(
		t,
		fixture.root,
		fixture.launcherPath,
		fixture.serverJarPath,
		fixture.propertiesPath,
		fixture.loaderJarPath,
		matching,
		stale,
		cacheEntry,
	)

	result := Discover(fixture.root)
	result.ObserveOwnership(fixture.root)
	installation := quiltTestInstallation(t, result)

	for _, owned := range []string{
		fixture.launcherPath,
		fixture.serverJarPath,
		fixture.propertiesPath,
		fixture.loaderJarPath,
		matching,
	} {
		quiltTestAssertManaged(t, installation, owned)
	}
	for _, unrelated := range []string{stale, cacheEntry} {
		for _, observed := range result.Installations {
			if quiltTestHasPath(observed, unrelated) {
				t.Fatalf(
					"receipt-owned unrelated file %s claimed by %s",
					unrelated,
					observed.Identity.Primary.StringQualified(),
				)
			}
		}
	}

	// The receipt still names the matching path, but the content no longer
	// names the observed game version, so a fresh scan must not claim it.
	writeFile(t, matching, []byte("not a server jar\n"))
	for _, observed := range Discover(fixture.root).Installations {
		if quiltTestHasPath(observed, matching) {
			t.Fatalf(
				"content-mismatched first-run payload %s still claimed",
				matching,
			)
		}
	}
}
