//go:build e2e

// Package init verifies that Lucy initializes manifests from real server
// workspaces, including modpack fixtures and composed launcher layouts.
package init

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/e2e/internal/testkit"
	"gopkg.in/yaml.v3"
)

// TestInitialize runs lucy init against each real sandbox composition and
// checks the installation identity that the CLI writes for it. The launcher
// path an operator selects is ephemeral; later observation must derive the
// entry point from the declared identity and the files on disk.
func TestInitialize(t *testing.T) {
	binary := testkit.Lucy(t)
	for _, id := range slices.Sorted(maps.Keys(initializeScenarios)) {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			scenario := initializeScenarios[id]
			sandbox := scenario.sandbox
			if sandbox == "" {
				sandbox = id
			}
			root, err := filepath.EvalSymlinks(testkit.CloneSandbox(t, sandbox))
			if err != nil {
				t.Fatalf("resolve clone root for %q: %v", sandbox, err)
			}
			// A local sandbox may carry intent from an earlier run; init must
			// start from the server workspace alone.
			removePaths(t, root, []string{documentpkg.ManifestFilename, documentpkg.LockFilename, ".lucy"})
			// Composition cases start from the files the modpack itself ships,
			// not from a previous installation left in the generated sandbox.
			removePaths(t, root, scenario.prune)
			unchanged := digests(t, root, scenario.unchanged)

			runner := commandRunner{
				binary: binary,
				home:   filepath.Join(t.TempDir(), "home"),
			}
			for _, command := range scenario.prepare {
				args := withEntryPoint(command.args, root, command.selectEntryPoint)
				stdout, stderr, err := runner.execute(t, root, args...)
				if err != nil {
					t.Fatalf(
						"prepare %s: %v\nstdout: %s\nstderr: %s",
						strings.Join(args, " "), err, stdout, stderr,
					)
				}
			}
			for _, artifact := range scenario.artifacts {
				copyArtifact(t, root, sandbox, artifact)
			}
			for _, link := range scenario.symlinks {
				createSymlink(t, root, link)
			}
			removePaths(t, root, scenario.removeBefore)

			args := []string{"init", "--no-interactive"}
			args = withEntryPoint(args, root, scenario.selectEntryPoint)
			stdout, stderr, err := runner.execute(t, root, args...)
			output := stdout + stderr
			if scenario.expectFailure {
				requireFailure(t, root, scenario, output, err)
				// Status still observes the unresolved layout; init's refusal
				// must not deny live-status postconditions.
				if scenario.status != nil {
					verifyStatus(t, runner, root, *scenario.status, scenario.informational)
				}
				verifyUnchanged(t, root, scenario.unchanged, unchanged)
				return
			}
			if err != nil {
				t.Fatalf(
					"lucy init: %v\nstdout: %s\nstderr: %s",
					err, stdout, stderr,
				)
			}
			if strings.Contains(output, "ERROR ") {
				t.Fatalf("init reported an error finding:\n%s", output)
			}
			requireInformational(t, stderr, root, scenario.informational)
			verifyInitialized(t, root, scenario)
			requireLockOmitsEntryPoint(t, root)
			verifyReceipt(t, root, scenario.wantReceipt)
			if scenario.status != nil {
				verifyStatus(t, runner, root, *scenario.status, scenario.informational)
			}
			verifyUnchanged(t, root, scenario.unchanged, unchanged)
		})
	}
}

// requireFailure asserts that init refused the directory, that an unresolved
// conflict names the exact selection command, and that the prior declaration
// and receipt stayed intact.
func requireFailure(
	t *testing.T,
	root string,
	scenario initializeScenario,
	output string,
	err error,
) {
	t.Helper()
	if err == nil {
		t.Fatalf("init succeeded where it must refuse:\n%s", output)
	}
	for _, candidate := range scenario.guidance {
		path := filepath.Join(root, filepath.FromSlash(candidate))
		command := "lucy init --entry-point '" + path + "'"
		if !strings.Contains(output, command) {
			t.Errorf("init failure does not name the selection command %q:\n%s", command, output)
		}
	}
	if scenario.minecraft != "" {
		verifyInitialized(t, root, scenario)
	}
	verifyReceipt(t, root, scenario.wantReceipt)
}

// requireInformational requires an INFO-level report line naming each path.
// The message text stays free to change; the severity and path are the
// consumer-visible contract.
func requireInformational(t *testing.T, output, root string, paths []string) {
	t.Helper()
	for _, candidate := range paths {
		path := filepath.Join(root, filepath.FromSlash(candidate))
		found := false
		for line := range strings.SplitSeq(output, "\n") {
			if strings.Contains(line, "INFO") && strings.Contains(line, path) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no INFO finding names %s:\n%s", path, output)
		}
	}
}

func verifyInitialized(
	t *testing.T,
	root string,
	scenario initializeScenario,
) {
	t.Helper()
	if scenario.coreVersion == "" {
		t.Fatalf("init scenario must pin the exact core version")
	}
	data, err := os.ReadFile(filepath.Join(root, documentpkg.ManifestFilename))
	if err != nil {
		t.Fatalf("read generated manifest: %v", err)
	}
	var document manifestDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode generated manifest: %v", err)
	}
	server := document.Server
	if server.Minecraft != scenario.minecraft ||
		server.Core.Product != scenario.product {
		t.Errorf(
			"manifest identity: got %s/%s@%s, want %s/%s",
			server.Minecraft, server.Core.Product, server.Core.ProductVersion,
			scenario.minecraft, scenario.product,
		)
	}
	if server.Core.ProductVersion != scenario.coreVersion {
		t.Errorf(
			"manifest core version: got %s, want %s",
			server.Core.ProductVersion, scenario.coreVersion,
		)
	}
	total := 0
	for _, packages := range server.Packages {
		total += len(packages)
	}
	if total < scenario.minPackages {
		t.Errorf("manifest packages: got %d, want at least %d", total, scenario.minPackages)
	}
	for loader, entries := range scenario.packages {
		for reference, version := range entries {
			if got := server.Packages[loader][reference]; got != version {
				t.Errorf(
					"manifest package %s [%s]: got %q, want %q",
					reference, loader, got, version,
				)
			}
		}
	}
	for loader, references := range scenario.absent {
		for _, reference := range references {
			if _, ok := server.Packages[loader][reference]; ok {
				t.Errorf("manifest lists client-only package %s [%s]", reference, loader)
			}
		}
	}
	locked := readLock(t, root)
	if locked.Server.Minecraft.String() != scenario.minecraft ||
		locked.Server.Product.String() != scenario.product {
		t.Errorf(
			"lock identity: got %s/%s, want %s/%s",
			locked.Server.Minecraft, locked.Server.Product,
			scenario.minecraft, scenario.product,
		)
	}
	if locked.Server.CoreVersion.String() != scenario.coreVersion {
		t.Errorf(
			"lock core version: got %s, want %s",
			locked.Server.CoreVersion, scenario.coreVersion,
		)
	}
	if len(locked.Packages) < scenario.minPackages {
		t.Errorf(
			"lock packages: got %d, want at least %d",
			len(locked.Packages), scenario.minPackages,
		)
	}
}

// requireLockOmitsEntryPoint inspects the written YAML rather than the decoder:
// the obsolete launcher path must not survive in the lock under any schema
// alias, and the assertion must not depend on the Go struct that decodes it.
func requireLockOmitsEntryPoint(t *testing.T, root string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, documentpkg.LockFilename))
	if err != nil {
		t.Fatalf("read generated lock: %v", err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode generated lock YAML: %v", err)
	}
	server, _ := document["server"].(map[string]any)
	if _, ok := server["entry_point"]; ok {
		t.Errorf("generated lock still records server.entry_point:\n%s", data)
	}
}

func verifyReceipt(t *testing.T, root string, want bool) {
	t.Helper()
	path := filepath.Join(root, ".lucy", "install.yaml")
	_, err := os.Stat(path)
	switch {
	case want && err != nil:
		t.Errorf("expected the installation receipt to survive init: %v", err)
	case !want && !os.IsNotExist(err):
		t.Errorf("init staged an installation receipt: %v", err)
	}
}

func verifyStatus(
	t *testing.T,
	runner commandRunner,
	root string,
	want statusExpectation,
	informational []string,
) {
	t.Helper()
	guarded := []string{documentpkg.ManifestFilename, documentpkg.LockFilename, ".lucy/install.yaml"}
	before := fileFingerprints(t, root, guarded)
	defer verifyFileFingerprints(t, root, guarded, before)
	stdout, stderr, err := runner.execute(t, root, "status", "--json-compact")
	if err != nil {
		t.Fatalf("lucy status: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("successful status JSON wrote stderr:\n%s", stderr)
	}
	var document statusDocument
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("decode live status JSON: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	requireStatusFindings(t, document.Warnings, root, informational)
	if document.Server == nil {
		t.Fatal("live status reported no server")
	}
	primary := document.Server.Product
	if (want.ecosystem != "" && primary.Ecosystem != want.ecosystem) ||
		(want.name != "" && primary.Name != want.name) ||
		(want.version != "" && primary.Version != want.version) {
		t.Errorf(
			"live status primary runtime = %s:%s@%s, want %s:%s@%s",
			primary.Ecosystem, primary.Name, primary.Version,
			want.ecosystem, want.name, want.version,
		)
	}
	if want.path != "" {
		requireStatusPath(t, "entry_point", document.Server.EntryPoint, want.path)
	}
	requireStatusFindings(t, document.Warnings, root, want.warning)
	for reference, version := range want.components {
		ecosystem, name, found := strings.Cut(reference, ":")
		if !found {
			continue
		}
		if got := componentVersion(document.Server.Components, ecosystem, name); got != version {
			t.Errorf(
				"live status component %s = %q, want %q",
				reference, got, version,
			)
		}
	}
}

// requireStatusPath compares the reported entry point with a workspace-relative
// fixture path.
func requireStatusPath(t *testing.T, label, got, want string) {
	t.Helper()
	expected := filepath.FromSlash(want)
	if filepath.Clean(got) != filepath.Clean(expected) {
		t.Errorf("live status %s = %q, want %q", label, got, expected)
	}
}

func requireStatusFindings(t *testing.T, warnings []string, root string, paths []string) {
	t.Helper()
	for _, candidate := range paths {
		absolute := filepath.Join(root, filepath.FromSlash(candidate))
		found := slices.ContainsFunc(warnings, func(message string) bool {
			return strings.Contains(message, absolute) || strings.Contains(message, filepath.ToSlash(candidate))
		})
		if !found {
			t.Errorf("no status finding names %s:\n%v", candidate, warnings)
		}
	}
}

func componentVersion(components []statusRef, ecosystem, name string) string {
	for _, component := range components {
		if component.Ecosystem == ecosystem && component.Name == name {
			return component.Version
		}
	}
	return ""
}

func readLock(t *testing.T, root string) documentpkg.Lock {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, documentpkg.LockFilename))
	if err != nil {
		t.Fatalf("read generated lock: %v", err)
	}
	locked, err := documentpkg.DecodeLock(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode generated lock: %v", err)
	}
	return *locked
}

func withEntryPoint(args []string, root, entryPoint string) []string {
	args = slices.Clone(args)
	if entryPoint != "" {
		args = append(
			args,
			"--entry-point",
			filepath.Join(root, filepath.FromSlash(entryPoint)),
		)
	}
	return args
}

func removePaths(t *testing.T, root string, paths []string) {
	t.Helper()
	for _, path := range paths {
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("remove %s: %v", path, err)
		}
	}
}

// createSymlink creates a real filesystem alias. Platform support is required;
// the test fails closed rather than copying the target.
func createSymlink(t *testing.T, root string, link initializeSymlink) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(link.link))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", link.link, err)
	}
	target := filepath.Join(root, filepath.FromSlash(link.target))
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("create symlink %s -> %s: %v", link.link, link.target, err)
	}
}

func copyArtifact(t *testing.T, root, sandbox string, artifact initializeArtifact) {
	t.Helper()
	sourceSandbox := artifact.sandbox
	if sourceSandbox == "" {
		sourceSandbox = sandbox
	}
	source := filepath.Join(
		testkit.Sandbox(t, sourceSandbox),
		filepath.FromSlash(artifact.source),
	)
	target := filepath.Join(root, filepath.FromSlash(artifact.dest))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", artifact.dest, err)
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatalf("open fixture %s: %v", source, err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(target)
	if err != nil {
		t.Fatalf("create %s: %v", artifact.dest, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatalf("copy fixture to %s: %v", artifact.dest, err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close %s: %v", artifact.dest, err)
	}
}

func digests(t *testing.T, root string, paths []string) map[string]string {
	t.Helper()
	sums := make(map[string]string, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		sum := sha256.Sum256(data)
		sums[path] = hex.EncodeToString(sum[:])
	}
	return sums
}

// fileFingerprint records whether a workspace file exists and its digest.
type fileFingerprint struct {
	present bool
	digest  string
}

// fileFingerprints snapshots declaration files so a read-only observation can
// prove it created or changed none of them.
func fileFingerprints(t *testing.T, root string, paths []string) map[string]fileFingerprint {
	t.Helper()
	fingerprints := make(map[string]fileFingerprint, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		switch {
		case os.IsNotExist(err):
			fingerprints[path] = fileFingerprint{}
		case err != nil:
			t.Fatalf("read %s: %v", path, err)
		default:
			sum := sha256.Sum256(data)
			fingerprints[path] = fileFingerprint{
				present: true,
				digest:  hex.EncodeToString(sum[:]),
			}
		}
	}
	return fingerprints
}

func verifyFileFingerprints(
	t *testing.T,
	root string,
	paths []string,
	before map[string]fileFingerprint,
) {
	t.Helper()
	after := fileFingerprints(t, root, paths)
	for _, path := range paths {
		if after[path] != before[path] {
			t.Errorf("%s changed during live status", path)
		}
	}
}

// verifyUnchanged proves the named fixture files survived preparation and
// initialization byte-for-byte.
func verifyUnchanged(t *testing.T, root string, paths []string, before map[string]string) {
	t.Helper()
	for _, path := range paths {
		if got := digests(t, root, []string{path})[path]; got != before[path] {
			t.Errorf("%s changed during initialization", path)
		}
	}
}

type statusDocument struct {
	Server *struct {
		Product    statusRef   `json:"product"`
		EntryPoint string      `json:"entry_point"`
		Components []statusRef `json:"components"`
	} `json:"server"`
	Warnings []string `json:"warnings"`
}

type statusRef struct {
	Ecosystem string `json:"ecosystem"`
	Name      string `json:"name"`
	Version   string `json:"version"`
}

type commandRunner struct {
	binary string
	home   string
}

// execute runs one Lucy command in root with an isolated home and captures
// stdout and stderr separately so status can be decoded without log noise.
func (r commandRunner) execute(t *testing.T, root string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	if err := os.MkdirAll(r.home, 0o755); err != nil {
		return "", "", fmt.Errorf("create command home: %w", err)
	}
	overrides := map[string]string{
		"HOME":            r.home,
		"USERPROFILE":     r.home,
		"XDG_CACHE_HOME":  filepath.Join(r.home, "cache"),
		"XDG_CONFIG_HOME": filepath.Join(r.home, "config"),
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return stdout.String(), stderr.String(), fmt.Errorf(
			"lucy %s: %w", strings.Join(args, " "), ctx.Err(),
		)
	}
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf(
			"lucy %s: %w", strings.Join(args, " "), err,
		)
	}
	return stdout.String(), stderr.String(), nil
}
