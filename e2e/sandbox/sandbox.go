// Command sandbox materializes server sandboxes from e2e/testdata/sandboxes.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mclucy/lucy/internal/toolchain"
	"gopkg.in/yaml.v3"
)

const userAgent = "lucy-sandbox/1.0 (+https://github.com/mclucy/lucy)"

type manifest struct {
	Version   int       `yaml:"version"`
	Sandboxes []sandbox `yaml:"sandboxes"`
}

// docsRefs accepts either a scalar string or a sequence of strings in YAML.
type docsRefs []string

func (d *docsRefs) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*d = []string{node.Value}
	case yaml.SequenceNode:
		var items []string
		if err := node.Decode(&items); err != nil {
			return err
		}
		*d = items
	default:
		return fmt.Errorf("docs must be a string or a list of strings")
	}
	return nil
}

type sandbox struct {
	ID          string   `yaml:"id"`
	Family      string   `yaml:"family"`
	GameVersion string   `yaml:"game_version"`
	Description string   `yaml:"description"`
	Docs        docsRefs `yaml:"docs,omitempty"`
	// EntryPoint is the launch target a sandbox must contain. An
	// installer-backed sandbox must produce it; a standalone sandbox pins it
	// directly. It is relative to the sandbox root.
	EntryPoint string     `yaml:"entry_point,omitempty"`
	Dirs       []string   `yaml:"dirs"`
	Artifacts  []artifact `yaml:"artifacts"`
}

type artifact struct {
	Dest string `yaml:"dest"`
	// SHA256 pins the expected content digest. Optional: without it, digest
	// verification is skipped.
	SHA256        string      `yaml:"sha256"`
	URL           string      `yaml:"url,omitempty"`
	MojangVersion string      `yaml:"mojang_version,omitempty"`
	FabricMeta    *fabricMeta `yaml:"fabric_meta,omitempty"`
	Purpur        *purpurSpec `yaml:"purpur,omitempty"`
	Manual        bool        `yaml:"manual,omitempty"`
	// Modpack marks a Modrinth pack archive. Generation writes the indexed
	// server files and the pack overrides into the sandbox root instead of
	// copying the archive.
	Modpack bool `yaml:"modpack,omitempty"`
	// InstallServer marks an official Forge/NeoForge installer. Generation
	// runs it with --installServer in the directory that holds dest, after
	// every artifact and fixture file has landed, so the sandbox carries the
	// generated launch target rather than only copied libraries.
	InstallServer bool `yaml:"install_server,omitempty"`
}

type fabricMeta struct {
	Game      string `yaml:"game"`
	Loader    string `yaml:"loader"`
	Installer string `yaml:"installer"`
}

type purpurSpec struct {
	Version string `yaml:"version"`
	Build   string `yaml:"build"`
}

type options struct {
	manifestPath string
	outRoot      string
	cacheDir     string
	manualDir    string
	fixturesRoot string
	only         string
	force        bool
	list         bool
}

func main() {
	opts := options{}
	flag.StringVar(
		&opts.manifestPath,
		"manifest",
		"e2e/testdata/sandboxes/sandboxes.yaml",
		"path to the sandboxes manifest",
	)
	flag.StringVar(&opts.outRoot, "out", ".sandboxes", "output root for generated sandboxes")
	defaultCache, _ := os.UserCacheDir()
	flag.StringVar(&opts.cacheDir, "cache", filepath.Join(defaultCache, "lucy-sandbox"), "download cache directory")
	flag.StringVar(&opts.manualDir, "manual-dir", "", "directory holding manually supplied artifacts (default <cache>/manual)")
	flag.StringVar(&opts.only, "only", "", "comma-separated sandbox ids to process")
	flag.BoolVar(&opts.force, "force", false, "regenerate even if the output directory already exists")
	flag.BoolVar(&opts.list, "list", false, "list sandboxes and exit")
	flag.Parse()

	if opts.manualDir == "" {
		opts.manualDir = filepath.Join(opts.cacheDir, "manual")
	}
	opts.fixturesRoot = filepath.Join(filepath.Dir(opts.manifestPath), "fixtures")

	data, err := os.ReadFile(opts.manifestPath)
	if err != nil {
		fatal("read manifest: %v", err)
	}
	var man manifest
	if err := yaml.Unmarshal(data, &man); err != nil {
		fatal("parse manifest: %v", err)
	}
	if err := validate(&man); err != nil {
		fatal("invalid manifest: %v", err)
	}
	if opts.list {
		listSandboxes(&man, opts.outRoot)
		return
	}
	if err := run(&man, opts); err != nil {
		fatal("%v", err)
	}
}

func validate(man *manifest) error {
	seen := map[string]bool{}
	for _, sb := range man.Sandboxes {
		if !filepath.IsLocal(sb.ID) || sb.ID == "." {
			return fmt.Errorf("invalid sandbox id %q: must be a local path below the output root", sb.ID)
		}
		if seen[sb.ID] {
			return fmt.Errorf("duplicate sandbox id %q", sb.ID)
		}
		seen[sb.ID] = true
		installers := 0
		for _, art := range sb.Artifacts {
			if art.Dest == "" {
				return fmt.Errorf("%s: artifact without dest", sb.ID)
			}
			if art.SHA256 != "" {
				if len(art.SHA256) != 64 {
					return fmt.Errorf("%s/%s: sha256 must be 64 hex chars", sb.ID, art.Dest)
				}
				if _, err := hex.DecodeString(art.SHA256); err != nil {
					return fmt.Errorf("%s/%s: bad sha256: %v", sb.ID, art.Dest, err)
				}
			}
			kinds := 0
			for _, set := range []bool{art.URL != "", art.MojangVersion != "", art.FabricMeta != nil, art.Purpur != nil, art.Manual} {
				if set {
					kinds++
				}
			}
			if kinds != 1 {
				return fmt.Errorf(
					"%s/%s: exactly one of url, mojang_version, fabric_meta, purpur, manual required",
					sb.ID, art.Dest,
				)
			}
			if art.Modpack && (art.URL == "" || art.SHA256 == "") {
				return fmt.Errorf("%s/%s: modpack requires url and sha256", sb.ID, art.Dest)
			}
			if art.InstallServer {
				installers++
				if art.Modpack {
					return fmt.Errorf("%s/%s: an installer cannot be a modpack", sb.ID, art.Dest)
				}
				if !strings.HasSuffix(strings.ToLower(art.Dest), ".jar") {
					return fmt.Errorf("%s/%s: installer dest must be a jar", sb.ID, art.Dest)
				}
			}
		}
		if installers > 1 {
			return fmt.Errorf("%s: at most one install_server artifact is supported", sb.ID)
		}
		hasEntryPoint := sb.EntryPoint != ""
		if installers == 1 && !hasEntryPoint {
			return fmt.Errorf("%s: install_server requires entry_point", sb.ID)
		}
		if hasEntryPoint {
			entryPoint := filepath.FromSlash(sb.EntryPoint)
			if !filepath.IsLocal(entryPoint) || entryPoint == "." {
				return fmt.Errorf("%s: entry_point must be a local path below the sandbox root", sb.ID)
			}
		}
	}
	return nil
}

func listSandboxes(man *manifest, outRoot string) {
	fmt.Printf("%-24s %-14s %-10s %-10s %s\n", "ID", "FAMILY", "GAME", "STATE", "DESCRIPTION")
	for _, sb := range man.Sandboxes {
		state := "missing"
		if _, err := os.Stat(filepath.Join(outRoot, sb.ID)); err == nil {
			state = "generated"
		}
		manual := ""
		for _, art := range sb.Artifacts {
			if art.Manual {
				manual = " (manual)"
				break
			}
		}
		fmt.Printf("%-24s %-14s %-10s %-10s %s%s\n",
			sb.ID, sb.Family, sb.GameVersion, state, sb.Description, manual)
	}
}

func run(man *manifest, opts options) error {
	selected := map[string]bool{}
	if opts.only != "" {
		for id := range strings.SplitSeq(opts.only, ",") {
			selected[strings.TrimSpace(id)] = true
		}
	}
	if err := os.MkdirAll(opts.cacheDir, 0o755); err != nil {
		return err
	}
	var failed []string
	for _, sb := range man.Sandboxes {
		if len(selected) > 0 && !selected[sb.ID] {
			continue
		}
		if err := generateSandbox(sb, opts); err != nil {
			fmt.Fprintf(os.Stderr, "[FAIL] %s: %v\n", sb.ID, err)
			failed = append(failed, sb.ID)
			continue
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed sandboxes: %s", strings.Join(failed, ", "))
	}
	return nil
}

type resolvedArtifact struct {
	artifact
	cachedPath string
}

func generateSandbox(sb sandbox, opts options) error {
	outDir := filepath.Join(opts.outRoot, sb.ID)
	if _, err := os.Stat(outDir); err == nil && !opts.force {
		fmt.Printf("[SKIP] %s (exists; use --force)\n", sb.ID)
		return nil
	}

	resolvedArtifacts := make([]resolvedArtifact, 0, len(sb.Artifacts))
	for _, art := range sb.Artifacts {
		path, err := ensureCached(sb.ID, art, opts)
		if err != nil {
			return fmt.Errorf("artifact %s: %w", art.Dest, err)
		}
		resolvedArtifacts = append(resolvedArtifacts, resolvedArtifact{artifact: art, cachedPath: path})
		fmt.Printf("[OK]   %s: %s\n", sb.ID, art.Dest)
	}

	if err := os.MkdirAll(opts.outRoot, 0o755); err != nil {
		return fmt.Errorf("create output root: %w", err)
	}
	stage, err := os.MkdirTemp(opts.outRoot, ".sandbox-*")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stage)
		}
	}()

	for _, dir := range sb.Dirs {
		if err := os.MkdirAll(filepath.Join(stage, dir), 0o755); err != nil {
			return err
		}
	}
	for _, ra := range resolvedArtifacts {
		if ra.Modpack {
			if err := materializeModpack(ra.cachedPath, stage, opts); err != nil {
				return fmt.Errorf("materialize modpack %s: %w", ra.Dest, err)
			}
			continue
		}
		dest := filepath.Join(stage, ra.Dest)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyFile(ra.cachedPath, dest); err != nil {
			return fmt.Errorf("install %s: %w", ra.Dest, err)
		}
	}
	fixturesDir := filepath.Join(opts.fixturesRoot, sb.ID)
	if _, err := os.Stat(fixturesDir); err == nil {
		if err := copyTree(fixturesDir, stage); err != nil {
			return fmt.Errorf("copy fixtures: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, ra := range resolvedArtifacts {
		if !ra.InstallServer {
			continue
		}
		if err := installServer(stage, ra); err != nil {
			return fmt.Errorf("%s: %w", sb.ID, err)
		}
		fmt.Printf("[INST] %s: %s\n", sb.ID, ra.Dest)
	}
	if sb.EntryPoint != "" {
		if err := verifyEntryPoint(stage, sb.EntryPoint); err != nil {
			return fmt.Errorf("%s: %w", sb.ID, err)
		}
	}

	// Publish only a complete, validated fixture. A failed download or
	// installer run leaves no directory that a later run would skip.
	if err := os.RemoveAll(outDir); err != nil {
		return fmt.Errorf("replace %s: %w", outDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(outDir), 0o755); err != nil {
		return err
	}
	if err := os.Rename(stage, outDir); err != nil {
		return fmt.Errorf("publish %s: %w", outDir, err)
	}
	published = true
	fmt.Printf("[GEN]  %s -> %s\n", sb.ID, outDir)
	return nil
}

// installerJavaMinimum is the oldest runtime that the pinned Forge and
// NeoForge installers support. The generator only runs the installer; it never
// launches the server, so the game version's own Java advisory does not apply.
const installerJavaMinimum = 17

// installServer runs one pinned loader installer in the directory that holds
// it. The installer is the only source of the generated argument sidecars,
// shim, and patched server jar; a copied universal jar is a runtime library,
// not a launch target.
func installServer(outDir string, ra resolvedArtifact) error {
	java, err := installerJava()
	if err != nil {
		return fmt.Errorf("installer %s: %w", ra.Dest, err)
	}
	target := filepath.Dir(filepath.Join(outDir, ra.Dest))
	// Run the copied installer under its fixture name so its own log and any
	// path it derives match a real `java -jar <installer> --installServer .`.
	cmd := exec.Command(java, "-jar", filepath.Base(ra.Dest), "--installServer", ".")
	cmd.Dir = target
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installer %s failed: %w\n%s", ra.Dest, err, outputTail(output.String()))
	}
	return nil
}

// installerJava picks the oldest installed runtime that the installers
// support. Preferring the oldest qualifying major keeps legacy installers off
// newer class-file versions while still accepting the current JDK.
func installerJava() (string, error) {
	inventory, err := toolchain.Discover(context.Background())
	if err != nil {
		return "", fmt.Errorf("discover Java runtimes: %w", err)
	}
	var chosen *toolchain.JavaInstallation
	for index := range inventory.Java {
		runtime := &inventory.Java[index]
		if runtime.Major < installerJavaMinimum {
			continue
		}
		if chosen == nil || runtime.Major < chosen.Major {
			chosen = runtime
		}
	}
	if chosen == nil {
		return "", fmt.Errorf(
			"no Java %d+ runtime found: install a JDK %d or newer and put java on PATH or set JAVA_HOME",
			installerJavaMinimum, installerJavaMinimum,
		)
	}
	return chosen.Java, nil
}

func outputTail(output string) string {
	const limit = 8 << 10
	output = strings.TrimSpace(output)
	if len(output) <= limit {
		return output
	}
	return "..." + output[len(output)-limit:]
}

// verifyEntryPoint proves that an installer produced its declared launch
// target, or that a pinned standalone launch jar is present and bootable.
func verifyEntryPoint(outDir, entryPoint string) error {
	path := filepath.Join(outDir, filepath.FromSlash(entryPoint))
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("launch target %s is missing: %w", entryPoint, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("launch target %s is not a regular file", entryPoint)
	}
	if info.Size() == 0 {
		return fmt.Errorf("launch target %s is empty", entryPoint)
	}
	if !strings.HasSuffix(strings.ToLower(entryPoint), ".jar") {
		return nil
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("launch target %s is not a readable jar: %w", entryPoint, err)
	}
	defer func() { _ = archive.Close() }()
	if len(archive.File) == 0 {
		return fmt.Errorf("launch target %s is an empty jar", entryPoint)
	}
	manifest, err := readZipEntry(&archive.Reader, "META-INF/MANIFEST.MF")
	if err != nil {
		return fmt.Errorf("launch target %s has no manifest: %w", entryPoint, err)
	}
	if manifestMainClass(manifest) == "" {
		return fmt.Errorf("launch target %s declares no Main-Class", entryPoint)
	}
	return nil
}

// manifestMainClass returns the declared Main-Class of a jar manifest, or ""
// when the attribute is absent. The manifest format is line oriented; a
// Main-Class value is short enough that Forge and NeoForge never fold it.
func manifestMainClass(manifest []byte) string {
	for line := range strings.SplitSeq(string(manifest), "\n") {
		key, value, ok := strings.Cut(strings.TrimSuffix(line, "\r"), ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "Main-Class") {
			continue
		}
		return strings.TrimSpace(value)
	}
	return ""
}

// modpackIndex is the subset of modrinth.index.json that server generation
// needs. Client-only entries carry env.server "unsupported".
type modpackIndex struct {
	Files []struct {
		Path   string `json:"path"`
		Hashes struct {
			SHA512 string `json:"sha512"`
		} `json:"hashes"`
		Env struct {
			Server string `json:"server"`
		} `json:"env"`
		Downloads []string `json:"downloads"`
	} `json:"files"`
}

// materializeModpack writes the server side of a Modrinth pack into outDir:
// every indexed server file lands at its declared path, then overrides and
// server-overrides unpack at the root.
func materializeModpack(packPath, outDir string, opts options) error {
	archive, err := zip.OpenReader(packPath)
	if err != nil {
		return err
	}
	defer func() { _ = archive.Close() }()

	data, err := readZipEntry(&archive.Reader, "modrinth.index.json")
	if err != nil {
		return fmt.Errorf("read pack index: %w", err)
	}
	var index modpackIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("decode pack index: %w", err)
	}

	for _, file := range index.Files {
		// Create+ 6.0.0 Alpha f marks one client-only jar as "unknown" for both
		// sides, which the pack format does not define. Skip it until the
		// upstream metadata is corrected: the jar crashes a dedicated server.
		// https://github.com/Sshmoob/Create-Plus/issues/80
		if file.Env.Server == "unsupported" || file.Env.Server == "unknown" {
			continue
		}
		if len(file.Downloads) == 0 {
			return fmt.Errorf("pack file %s has no download", file.Path)
		}
		cached, err := fetchDigest(file.Downloads[0], file.Hashes.SHA512, opts)
		if err != nil {
			return fmt.Errorf("pack file %s: %w", file.Path, err)
		}
		if err := writePackFile(outDir, file.Path, func(dest string) error {
			return copyFile(cached, dest)
		}); err != nil {
			return fmt.Errorf("pack file %s: %w", file.Path, err)
		}
	}

	// The pack format applies generic overrides first, then server overrides.
	// Archive order carries no layer meaning, so the two prefixes get their own
	// pass; a server file for a path always wins.
	for _, prefix := range []string{"overrides/", "server-overrides/"} {
		for _, entry := range archive.File {
			name, ok := strings.CutPrefix(entry.Name, prefix)
			if !ok || name == "" {
				continue
			}
			if err := writePackFile(outDir, name, func(dest string) error {
				if entry.FileInfo().IsDir() {
					return os.MkdirAll(dest, 0o755)
				}
				return extractZipEntry(entry, dest)
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// writePackFile resolves a pack-relative path below outDir, creates its parent
// directory, then hands the destination to write. Pack paths use forward
// slashes and must stay inside the sandbox root, so this is the single guard
// between archive content and the filesystem.
func writePackFile(outDir, name string, write func(string) error) error {
	relative := filepath.FromSlash(name)
	if relative == "." || strings.Contains(name, `\`) || !filepath.IsLocal(relative) {
		return fmt.Errorf("unsafe pack path %q", name)
	}
	dest := filepath.Join(outDir, relative)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return write(dest)
}

func readZipEntry(reader *zip.Reader, name string) ([]byte, error) {
	file, err := reader.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(file)
}

func extractZipEntry(entry *zip.File, dest string) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	mode := entry.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	target, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
}

// fetchDigest caches one pack file under its sha512 digest. The digest is
// mandatory: pack indexes always publish one, and it is the only integrity
// check for downloads that do not share the sandbox manifest.
func fetchDigest(url, sha512Hex string, opts options) (string, error) {
	key := strings.ToLower(sha512Hex)
	if len(key) != 128 {
		return "", fmt.Errorf("pack file has no usable sha512")
	}
	if _, err := hex.DecodeString(key); err != nil {
		return "", fmt.Errorf("bad sha512: %w", err)
	}
	cached := filepath.Join(opts.cacheDir, key[:2], key)
	if _, err := os.Stat(cached); err == nil {
		return cached, nil
	}
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(opts.cacheDir, "modpack-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := downloadTo(url, tmp); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	digest, err := fileSHA512(tmpName)
	if err != nil {
		return "", err
	}
	if digest != key {
		return "", fmt.Errorf("sha512 mismatch: got %s want %s", digest, key)
	}
	if err := os.Rename(tmpName, cached); err != nil {
		return "", err
	}
	return cached, nil
}

func fileSHA512(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	h := sha512.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ensureCached(sandboxID string, art artifact, opts options) (string, error) {
	key := strings.ToLower(art.SHA256)
	if key == "" {
		sum := sha256.Sum256([]byte(sourceDescriptor(sandboxID, art)))
		key = "src-" + hex.EncodeToString(sum[:])
	}
	cached := filepath.Join(opts.cacheDir, key[:2], key)
	if _, err := os.Stat(cached); err == nil {
		return cached, nil
	}
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp(opts.cacheDir, "download-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	switch {
	case art.Manual:
		_ = tmp.Close()
		source := filepath.Join(opts.manualDir, sandboxID, filepath.Base(art.Dest))
		if _, err := os.Stat(source); err != nil {
			if art.SHA256 == "" {
				return "", fmt.Errorf("manual artifact missing: place the file at %s", source)
			}
			return "", fmt.Errorf(
				"manual artifact missing: place the file at %s (expected sha256 %s)",
				source, key,
			)
		}
		if err := copyFile(source, tmpName); err != nil {
			return "", err
		}
	case art.URL != "":
		if err := downloadTo(art.URL, tmp); err != nil {
			_ = tmp.Close()
			return "", err
		}
		_ = tmp.Close()
	case art.MojangVersion != "":
		url, err := resolveMojangServer(art.MojangVersion)
		if err != nil {
			_ = tmp.Close()
			return "", err
		}
		if err := downloadTo(url, tmp); err != nil {
			_ = tmp.Close()
			return "", err
		}
		_ = tmp.Close()
	case art.FabricMeta != nil:
		url := fmt.Sprintf(
			"https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar",
			art.FabricMeta.Game, art.FabricMeta.Loader, art.FabricMeta.Installer,
		)
		if err := downloadTo(url, tmp); err != nil {
			_ = tmp.Close()
			return "", err
		}
		_ = tmp.Close()
	case art.Purpur != nil:
		url := fmt.Sprintf(
			"https://api.purpurmc.org/v2/purpur/%s/%s/download",
			art.Purpur.Version, art.Purpur.Build,
		)
		if err := downloadTo(url, tmp); err != nil {
			_ = tmp.Close()
			return "", err
		}
		_ = tmp.Close()
	}

	if art.SHA256 != "" {
		digest, err := fileSHA256(tmpName)
		if err != nil {
			return "", err
		}
		if digest != key {
			return "", fmt.Errorf("sha256 mismatch: got %s want %s", digest, key)
		}
	}
	if err := os.Rename(tmpName, cached); err != nil {
		return "", err
	}
	return cached, nil
}

// sourceDescriptor names where an artifact's bytes come from. Digest-less
// artifacts key their cache entries by the hash of this descriptor, prefixed
// src- to keep them disjoint from content digests.
func sourceDescriptor(sandboxID string, art artifact) string {
	switch {
	case art.URL != "":
		return art.URL
	case art.MojangVersion != "":
		return "mojang:" + art.MojangVersion
	case art.FabricMeta != nil:
		return fmt.Sprintf(
			"fabric:%s/%s/%s",
			art.FabricMeta.Game, art.FabricMeta.Loader, art.FabricMeta.Installer,
		)
	case art.Purpur != nil:
		return fmt.Sprintf("purpur:%s/%s", art.Purpur.Version, art.Purpur.Build)
	default:
		return "manual:" + sandboxID + "/" + filepath.Base(art.Dest)
	}
}

func resolveMojangServer(version string) (string, error) {
	metaURL := "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	var manifestResp struct {
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := getJSON(metaURL, &manifestResp); err != nil {
		return "", fmt.Errorf("mojang manifest: %w", err)
	}
	var entryURL string
	for _, v := range manifestResp.Versions {
		if v.ID == version {
			entryURL = v.URL
			break
		}
	}
	if entryURL == "" {
		return "", fmt.Errorf("mojang version %q not found", version)
	}
	var versionJSON struct {
		Downloads map[string]struct {
			URL string `json:"url"`
		} `json:"downloads"`
	}
	if err := getJSON(entryURL, &versionJSON); err != nil {
		return "", fmt.Errorf("mojang version %s: %w", version, err)
	}
	server, ok := versionJSON.Downloads["server"]
	if !ok || server.URL == "" {
		return "", fmt.Errorf("mojang version %s has no server download", version)
	}
	return server.URL, nil
}

func getJSON(url string, target any) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return json.UnmarshalRead(resp.Body, target)
}

func downloadTo(url string, tmp *os.File) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	var lastErr error
	for range 3 {
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := tmp.Truncate(0); err != nil {
			return err
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := client.Do(req)
		if err == nil {
			if resp.StatusCode != http.StatusOK {
				_ = resp.Body.Close()
				lastErr = fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
				continue
			}
			_, err = io.Copy(tmp, resp.Body)
			_ = resp.Body.Close()
			if err == nil {
				return nil
			}
			lastErr = err
		} else {
			lastErr = err
		}
	}
	return lastErr
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmpDst := dst + ".sandbox-tmp"
	out, err := os.Create(tmpDst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpDst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpDst)
		return err
	}
	return os.Rename(tmpDst, dst)
}

func copyTree(srcRoot, dstRoot string) error {
	return filepath.WalkDir(srcRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstRoot, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "sandbox: "+format+"\n", args...)
	os.Exit(1)
}
