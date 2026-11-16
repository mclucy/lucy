//go:build e2e

// Package install verifies that a generated Quilt server installs, installs
// its loader from the Quilt server profile, and actually boots.
package install

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/e2e/internal/testkit"
	"github.com/mclucy/lucy/internal/toolchain"
	serverpkg "github.com/mclucy/lucy/server"
)

const (
	quiltMinecraft = "1.20.1"
	// Quilt 0.23.1 exposes Fabric Loader 0.15.3, which supplies the
	// MixinExtras version QFAPI 7.1.2 requires on Minecraft 1.20.1.
	quiltLoader = "0.23.1"

	// The pinned QFAPI/QSL release bundles the client-only Fabric API modules
	// Quilt Minims requires, so adding it must not pull a standalone Fabric API
	// package onto a dedicated server.
	quiltedFabricAPIVersion   = "7.1.2+0.87.0-1.20.1"
	quiltedFabricAPIProjectID = "qvIfYCYJ"
	fabricAPIProjectID        = "P7dR8mSH"

	// Friends & Foes declares a required Fabric API dependency. QFAPI provides
	// that API on Quilt, so adding the mod must not pull a second, standalone
	// Fabric API artifact.
	friendsAndFoesVersion   = "fabric-mc1.20.1-3.0.9"
	friendsAndFoesProjectID = "POQ2i9zu"

	quiltLauncher      = "quilt-server-launch.jar"
	quiltLauncherProps = "quilt-server-launcher.properties"
	quiltLoaderLibrary = "libraries/org/quiltmc/quilt-loader/" + quiltLoader + "/quilt-loader-" + quiltLoader + ".jar"

	quiltLauncherMainClass = "org.quiltmc.loader.impl.launch.server.QuiltServerLauncher"
	quiltKnotMainClass     = "org.quiltmc.loader.impl.launch.knot.KnotServer"
)

// TestQuiltLifecycle creates a Quilt server from the real CLI, installs it,
// checks the persisted identity and the generated launcher contract, adds the
// pinned QFAPI/QSL and Friends & Foes packages through the CLI, then starts
// the mixed server and waits for Quilt Loader and the world to come up.
func TestQuiltLifecycle(t *testing.T) {
	binary := testkit.Lucy(t)
	temporary := t.TempDir()
	root := filepath.Join(temporary, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	runner := commandRunner{
		binary: binary,
		home:   filepath.Join(temporary, "home"),
	}
	invoke := func(args ...string) string {
		t.Helper()
		output, err := runner.execute(t.Context(), root, args...)
		if err != nil {
			t.Fatalf("lucy %s: %v\n%s", strings.Join(args, " "), err, output)
		}
		return output
	}

	invoke(
		"create",
		"--minecraft", quiltMinecraft,
		"--core", "quilt@"+quiltLoader,
		"--no-install",
		"--no-interactive",
	)
	invoke("install", "--locked", "--overwrite", "--no-interactive")

	verifyQuiltManifest(t, root)
	verifyQuiltLock(t, root)
	verifyQuiltLauncher(t, root)

	invoke("add", "qsl@"+quiltedFabricAPIVersion, "--no-interactive")
	invoke("add", "friends-and-foes@"+friendsAndFoesVersion, "--no-interactive")
	verifyQuiltLock(t, root)
	verifyQuiltedFabricAPI(t, root)
	verifyFriendsAndFoes(t, root)

	writeServerFiles(t, root)
	java := selectJava(t)
	output := bootQuilt(t, root, java)
	for _, expected := range []string{
		"Quilt Loader " + quiltLoader,
		"Done (",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("server output does not contain %q:\n%s", expected, output)
		}
	}
}

// verifyQuiltManifest checks the declaration the create wrote before install.
func verifyQuiltManifest(t *testing.T, root string) {
	t.Helper()
	manifest := readManifest(t, root)
	if got := manifest.Server.Core.Product; got != serverpkg.ProductQuilt {
		t.Errorf("manifest product = %q, want %q", got, serverpkg.ProductQuilt)
	}
	if got := manifest.Server.Core.ProductVersion.String(); got != quiltLoader {
		t.Errorf("manifest core version = %q, want %q", got, quiltLoader)
	}
	if got := manifest.Server.Minecraft.String(); got != quiltMinecraft {
		t.Errorf("manifest Minecraft = %q, want %q", got, quiltMinecraft)
	}
}

// verifyQuiltLock checks the locked identity and the bootstrap adapter the
// launcher files come from.
func verifyQuiltLock(t *testing.T, root string) {
	t.Helper()
	locked := readLock(t, root)
	if got := locked.Server.Product; got != serverpkg.ProductQuilt {
		t.Errorf("lock product = %q, want %q", got, serverpkg.ProductQuilt)
	}
	if got := locked.Server.Minecraft.String(); got != quiltMinecraft {
		t.Errorf("lock Minecraft = %q, want %q", got, quiltMinecraft)
	}
	if got := locked.Server.CoreVersion.String(); got != quiltLoader {
		t.Errorf("lock core version = %q, want %q", got, quiltLoader)
	}
	if got := locked.Server.Bootstrapper; got != serverpkg.BootstrapMethodQuilt {
		t.Errorf("lock bootstrapper = %q, want %q", got, serverpkg.BootstrapMethodQuilt)
	}
	// A profile-based install pins the vanilla server and the hashed and
	// intermediary mappings the loader needs; a bare loader JAR cannot boot.
	wantedPrefixes := map[string]bool{
		"libraries/org/quiltmc/hashed/":        false,
		"libraries/net/fabricmc/intermediary/": false,
	}
	hasServerJar := false
	for _, input := range locked.Server.Inputs {
		if input.Path == "server.jar" {
			hasServerJar = true
		}
		for prefix := range wantedPrefixes {
			if strings.HasPrefix(input.Path, prefix) {
				wantedPrefixes[prefix] = true
			}
		}
	}
	if !hasServerJar {
		t.Error("lock inputs do not pin the vanilla server.jar")
	}
	for prefix, present := range wantedPrefixes {
		if !present {
			t.Errorf("lock inputs do not include a %s library", prefix)
		}
	}
}

// verifyQuiltLauncher checks the generated files that make the install
// bootable: the vanilla JAR selector, the loader library, and the launcher JAR
// with its main class and embedded KnotServer entry.
func verifyQuiltLauncher(t *testing.T, root string) {
	t.Helper()
	for _, path := range []string{quiltLauncher, quiltLauncherProps, quiltLoaderLibrary, "server.jar"} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("installed file %s: %v", path, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("installed file %s is not a non-empty regular file", path)
		}
	}

	properties, err := os.ReadFile(filepath.Join(root, quiltLauncherProps))
	if err != nil {
		t.Fatalf("read %s: %v", quiltLauncherProps, err)
	}
	if got := strings.TrimSpace(string(properties)); got != "serverJar=server.jar" {
		t.Errorf("%s = %q, want %q", quiltLauncherProps, got, "serverJar=server.jar")
	}

	archive, err := zip.OpenReader(filepath.Join(root, quiltLauncher))
	if err != nil {
		t.Fatalf("open %s: %v", quiltLauncher, err)
	}
	defer func() { _ = archive.Close() }()

	manifest := readZipEntry(t, &archive.Reader, "META-INF/MANIFEST.MF")
	unfolded := unfoldManifest(manifest)
	for _, expected := range []string{
		"Main-Class: " + quiltLauncherMainClass,
		quiltLoaderLibrary,
	} {
		if !strings.Contains(unfolded, expected) {
			t.Errorf("%s manifest does not contain %q:\n%s", quiltLauncher, expected, unfolded)
		}
	}

	embedded := readZipEntry(t, &archive.Reader, "quilt-server-launch.properties")
	if got := strings.TrimSpace(embedded); got != "launch.mainClass="+quiltKnotMainClass {
		t.Errorf(
			"%s embedded properties = %q, want %q",
			quiltLauncher, got, "launch.mainClass="+quiltKnotMainClass,
		)
	}
}

// unfoldManifest removes the 72-column continuation breaks from a jar manifest
// so wrapped values can be compared as one line.
func unfoldManifest(manifest string) string {
	manifest = strings.ReplaceAll(manifest, "\r\n", "\n")
	return strings.ReplaceAll(manifest, "\n ", "")
}

func readZipEntry(t *testing.T, archive *zip.Reader, name string) string {
	t.Helper()
	entry, err := archive.Open(name)
	if err != nil {
		t.Fatalf("open %s in launcher: %v", name, err)
	}
	defer func() { _ = entry.Close() }()
	data, err := io.ReadAll(entry)
	if err != nil {
		t.Fatalf("read %s in launcher: %v", name, err)
	}
	return string(data)
}

func readManifest(t *testing.T, root string) *documentpkg.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, documentpkg.ManifestFilename))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifest, err := documentpkg.DecodeManifest(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	return manifest
}

func readLock(t *testing.T, root string) *documentpkg.Lock {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, documentpkg.LockFilename))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	locked, err := documentpkg.DecodeLock(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode lock: %v", err)
	}
	return locked
}

// verifyQuiltedFabricAPI proves the CLI recorded qsl as a direct Quilt
// requirement, installed its artifact, and did not add a standalone Fabric API
// package. The pinned QFAPI release bundles the Fabric API modules the pack
// needs, which is what lets this dedicated server boot.
func verifyQuiltedFabricAPI(t *testing.T, root string) {
	t.Helper()
	manifest := readManifest(t, root)
	requirement, ok := manifest.Server.Packages[catalog.EcosystemQuilt]["modrinth:qsl"]
	if !ok {
		t.Fatalf("manifest does not declare modrinth:qsl under the Quilt ecosystem")
	}
	if got := requirement.Selector.String(); got != quiltedFabricAPIVersion {
		t.Errorf("modrinth:qsl selector = %q, want %q", got, quiltedFabricAPIVersion)
	}

	locked := readLock(t, root)
	packageIndex := -1
	for index := range locked.Packages {
		pkg := &locked.Packages[index]
		filename := strings.ToLower(pkg.Filename)
		if pkg.ProjectID == quiltedFabricAPIProjectID ||
			strings.HasPrefix(filename, "qfapi-") {
			packageIndex = index
		}
		if pkg.ProjectID == fabricAPIProjectID ||
			strings.Contains(filename, "fabric-api") {
			t.Errorf("install added a standalone Fabric API package: %s", pkg.Filename)
		}
	}
	if packageIndex < 0 {
		t.Fatalf(
			"lock has no QFAPI/QSL package (project %s):\n%v",
			quiltedFabricAPIProjectID, locked.Packages,
		)
	}
	destination, err := locked.PackageDestination(&locked.Packages[packageIndex])
	if err != nil {
		t.Fatalf("resolve QFAPI package destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(destination))); err != nil {
		t.Errorf("QFAPI package is missing at %s: %v", destination, err)
	}

	entries, err := os.ReadDir(filepath.Join(root, "mods"))
	if err != nil {
		t.Fatalf("read mods directory: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Name()), "fabric-api") {
			t.Errorf("mods directory contains standalone Fabric API file %s", entry.Name())
		}
	}
}

// verifyFriendsAndFoes proves the second package is recorded and installed
// beside QFAPI. Its required Fabric API dependency is satisfied by QFAPI, which
// verifyQuiltedFabricAPI separately proves did not create a standalone package.
func verifyFriendsAndFoes(t *testing.T, root string) {
	t.Helper()
	manifest := readManifest(t, root)
	requirement, ok := manifest.Server.Packages[catalog.EcosystemQuilt]["modrinth:friends-and-foes"]
	if !ok {
		t.Fatalf("manifest does not declare modrinth:friends-and-foes under the Quilt ecosystem")
	}
	if got := requirement.Selector.String(); got != friendsAndFoesVersion {
		t.Errorf(
			"modrinth:friends-and-foes selector = %q, want %q",
			got, friendsAndFoesVersion,
		)
	}

	locked := readLock(t, root)
	packageIndex := -1
	for index := range locked.Packages {
		pkg := &locked.Packages[index]
		if pkg.ProjectID == friendsAndFoesProjectID ||
			strings.HasPrefix(strings.ToLower(pkg.Filename), "friendsandfoes-") {
			packageIndex = index
		}
	}
	if packageIndex < 0 {
		t.Fatalf(
			"lock has no friends-and-foes package (project %s):\n%v",
			friendsAndFoesProjectID, locked.Packages,
		)
	}
	destination, err := locked.PackageDestination(&locked.Packages[packageIndex])
	if err != nil {
		t.Fatalf("resolve friends-and-foes package destination: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(destination))); err != nil {
		t.Errorf("friends-and-foes package is missing at %s: %v", destination, err)
	}
}

// writeServerFiles accepts the EULA and keeps the first boot small enough for
// an e2e run.
func writeServerFiles(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "eula.txt"), []byte("eula=true\n"), 0o644); err != nil {
		t.Fatalf("write eula.txt: %v", err)
	}
	properties := strings.Join([]string{
		"online-mode=false",
		"level-type=minecraft\\:flat",
		"generate-structures=false",
		"spawn-monsters=false",
		"view-distance=2",
		"simulation-distance=2",
		"max-players=1",
		"max-tick-time=-1",
		"enable-command-block=false",
		"motd=lucy e2e",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "server.properties"), []byte(properties), 0o644); err != nil {
		t.Fatalf("write server.properties: %v", err)
	}
}

// selectJava picks the oldest installed runtime the Quilt loader supports.
func selectJava(t *testing.T) string {
	t.Helper()
	inventory, err := toolchain.Discover(t.Context())
	if err != nil {
		t.Fatalf("discover Java runtimes: %v", err)
	}
	chosen := ""
	major := 0
	for _, installation := range inventory.Java {
		if installation.Major < 17 {
			continue
		}
		if chosen == "" || installation.Major < major {
			chosen = installation.Java
			major = installation.Major
		}
	}
	if chosen == "" {
		t.Fatalf(
			"no Java 17 or newer runtime found: install a JDK 17 and put java on PATH or set JAVA_HOME",
		)
	}
	return chosen
}

// bootQuilt starts the generated server, waits for the world to finish
// loading, asks it to stop, and returns everything it printed.
func bootQuilt(t *testing.T, root, java string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		java,
		"-Xms512M",
		"-Xmx1G",
		"-jar",
		quiltLauncher,
		"nogui",
	)
	cmd.Dir = root
	cmd.WaitDelay = 10 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("open server stdin: %v", err)
	}
	watcher := &outputWatcher{
		needle: "Done (",
		found:  make(chan struct{}),
	}
	cmd.Stdout = watcher
	cmd.Stderr = watcher
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Quilt server: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	select {
	case <-watcher.found:
	case err := <-waited:
		t.Fatalf("Quilt server exited before startup: %v\n%s", err, watcher.String())
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-waited
		t.Fatalf("Quilt server did not start in time:\n%s", watcher.String())
	}

	if _, err := io.WriteString(stdin, "stop\n"); err != nil {
		t.Fatalf("send stop to Quilt server: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("close server stdin: %v", err)
	}
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("Quilt server stopped with an error: %v\n%s", err, watcher.String())
		}
	case <-time.After(2 * time.Minute):
		_ = cmd.Process.Kill()
		<-waited
		t.Fatalf("Quilt server did not stop:\n%s", watcher.String())
	}
	return watcher.String()
}

// outputWatcher captures server output and signals the first time the
// accumulated stream contains a startup marker.
type outputWatcher struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	needle string
	once   sync.Once
	found  chan struct{}
}

func (w *outputWatcher) Write(data []byte) (int, error) {
	w.mu.Lock()
	_, _ = w.buffer.Write(data)
	matched := strings.Contains(w.buffer.String(), w.needle)
	w.mu.Unlock()
	if matched {
		w.once.Do(func() { close(w.found) })
	}
	return len(data), nil
}

func (w *outputWatcher) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

type commandRunner struct {
	binary string
	home   string
}

func (r commandRunner) execute(ctx context.Context, root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if err := os.MkdirAll(r.home, 0o755); err != nil {
		return "", fmt.Errorf("create command home: %w", err)
	}
	overrides := map[string]string{
		"HOME":            r.home,
		"USERPROFILE":     r.home,
		"XDG_CACHE_HOME":  filepath.Join(r.home, "cache"),
		"XDG_CONFIG_HOME": filepath.Join(r.home, "config"),
		"LUCY_TELEMETRY":  "off",
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := overrides[strings.ToUpper(key)]; overridden {
			continue
		}
		environment = append(environment, entry)
	}
	for key, value := range overrides {
		environment = append(environment, key+"="+value)
	}
	args = append(args, "--no-style")
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = root
	cmd.Env = environment
	cmd.WaitDelay = 5 * time.Second
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), fmt.Errorf("lucy %s: %w\n%s", strings.Join(args, " "), ctx.Err(), output)
	}
	if err != nil {
		return string(output), fmt.Errorf("lucy %s: %w\n%s", strings.Join(args, " "), err, output)
	}
	return string(output), nil
}
