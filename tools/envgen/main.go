// Command envgen generates sandbox server environments from the manifest in
// testdata/environments into .sandboxes/. See docs/shared/sandbox-environments.md.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const userAgent = "lucy-envgen/1.0 (+https://github.com/mclucy/lucy)"

type manifest struct {
	Version      int           `yaml:"version"`
	Environments []environment `yaml:"environments"`
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

type environment struct {
	ID            string     `yaml:"id"`
	Family        string     `yaml:"family"`
	GameVersion   string     `yaml:"game_version"`
	Description   string     `yaml:"description"`
	Docs          docsRefs   `yaml:"docs,omitempty"`
	Dirs          []string   `yaml:"dirs"`
	Artifacts     []artifact `yaml:"artifacts"`
	InstallerHint string     `yaml:"installer_hint,omitempty"`
}

type artifact struct {
	Dest          string      `yaml:"dest"`
	SHA256        string      `yaml:"sha256"`
	URL           string      `yaml:"url,omitempty"`
	MojangVersion string      `yaml:"mojang_version,omitempty"`
	FabricMeta    *fabricMeta `yaml:"fabric_meta,omitempty"`
	Purpur        *purpurSpec `yaml:"purpur,omitempty"`
	Manual        bool        `yaml:"manual,omitempty"`
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
	seedsRoot    string
	only         string
	force        bool
	list         bool
}

func main() {
	opts := options{}
	flag.StringVar(&opts.manifestPath, "manifest", "testdata/environments/environments.yaml", "path to environments manifest")
	flag.StringVar(&opts.outRoot, "out", ".sandboxes", "output root for generated environments")
	defaultCache, _ := os.UserCacheDir()
	flag.StringVar(&opts.cacheDir, "cache", filepath.Join(defaultCache, "lucy-envgen"), "download cache directory")
	flag.StringVar(&opts.manualDir, "manual-dir", "", "directory holding manually supplied artifacts (default <cache>/manual)")
	flag.StringVar(&opts.only, "only", "", "comma-separated environment ids to process")
	flag.BoolVar(&opts.force, "force", false, "regenerate even if the output directory already exists")
	flag.BoolVar(&opts.list, "list", false, "list environments and exit")
	flag.Parse()

	if opts.manualDir == "" {
		opts.manualDir = filepath.Join(opts.cacheDir, "manual")
	}
	opts.seedsRoot = filepath.Join(filepath.Dir(opts.manifestPath), "seeds")

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
		listEnvironments(&man, opts.outRoot)
		return
	}
	if err := run(&man, opts); err != nil {
		fatal("%v", err)
	}
}

func validate(man *manifest) error {
	seen := map[string]bool{}
	for _, env := range man.Environments {
		if env.ID == "" {
			return errors.New("environment without id")
		}
		if seen[env.ID] {
			return fmt.Errorf("duplicate environment id %q", env.ID)
		}
		seen[env.ID] = true
		for _, art := range env.Artifacts {
			if art.Dest == "" {
				return fmt.Errorf("%s: artifact without dest", env.ID)
			}
			if len(art.SHA256) != 64 {
				return fmt.Errorf("%s/%s: sha256 must be 64 hex chars", env.ID, art.Dest)
			}
			if _, err := hex.DecodeString(art.SHA256); err != nil {
				return fmt.Errorf("%s/%s: bad sha256: %v", env.ID, art.Dest, err)
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
					env.ID, art.Dest,
				)
			}
		}
	}
	return nil
}

func listEnvironments(man *manifest, outRoot string) {
	fmt.Printf("%-24s %-14s %-10s %-10s %s\n", "ID", "FAMILY", "GAME", "STATE", "DESCRIPTION")
	for _, env := range man.Environments {
		state := "missing"
		if _, err := os.Stat(filepath.Join(outRoot, env.ID)); err == nil {
			state = "generated"
		}
		manual := ""
		for _, art := range env.Artifacts {
			if art.Manual {
				manual = " (manual)"
				break
			}
		}
		fmt.Printf("%-24s %-14s %-10s %-10s %s%s\n",
			env.ID, env.Family, env.GameVersion, state, env.Description, manual)
	}
}

func run(man *manifest, opts options) error {
	selected := map[string]bool{}
	if opts.only != "" {
		for _, id := range strings.Split(opts.only, ",") {
			selected[strings.TrimSpace(id)] = true
		}
	}
	if err := os.MkdirAll(opts.cacheDir, 0o755); err != nil {
		return err
	}
	var failed []string
	for _, env := range man.Environments {
		if len(selected) > 0 && !selected[env.ID] {
			continue
		}
		if err := generateEnvironment(env, opts); err != nil {
			fmt.Fprintf(os.Stderr, "[FAIL] %s: %v\n", env.ID, err)
			failed = append(failed, env.ID)
			continue
		}
		if env.InstallerHint != "" {
			fmt.Printf("[HINT] %s: %s\n", env.ID, env.InstallerHint)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed environments: %s", strings.Join(failed, ", "))
	}
	return nil
}

func generateEnvironment(env environment, opts options) error {
	outDir := filepath.Join(opts.outRoot, env.ID)
	if _, err := os.Stat(outDir); err == nil && !opts.force {
		fmt.Printf("[SKIP] %s (exists; use --force)\n", env.ID)
		return nil
	}

	type resolved struct {
		artifact
		cachedPath string
	}
	resolvedArtifacts := make([]resolved, 0, len(env.Artifacts))
	for _, art := range env.Artifacts {
		path, err := ensureCached(env.ID, art, opts)
		if err != nil {
			return fmt.Errorf("artifact %s: %w", art.Dest, err)
		}
		resolvedArtifacts = append(resolvedArtifacts, resolved{artifact: art, cachedPath: path})
		fmt.Printf("[OK]   %s: %s\n", env.ID, art.Dest)
	}

	for _, dir := range env.Dirs {
		if err := os.MkdirAll(filepath.Join(outDir, dir), 0o755); err != nil {
			return err
		}
	}
	for _, ra := range resolvedArtifacts {
		dest := filepath.Join(outDir, ra.Dest)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := copyFile(ra.cachedPath, dest); err != nil {
			return fmt.Errorf("install %s: %w", ra.Dest, err)
		}
	}
	seedsDir := filepath.Join(opts.seedsRoot, env.ID)
	if _, err := os.Stat(seedsDir); err == nil {
		if err := copyTree(seedsDir, outDir); err != nil {
			return fmt.Errorf("copy seeds: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Printf("[GEN]  %s -> %s\n", env.ID, outDir)
	return nil
}

func ensureCached(envID string, art artifact, opts options) (string, error) {
	key := strings.ToLower(art.SHA256)
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
		source := filepath.Join(opts.manualDir, envID, filepath.Base(art.Dest))
		if _, err := os.Stat(source); err != nil {
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

	digest, err := fileSHA256(tmpName)
	if err != nil {
		return "", err
	}
	if digest != key {
		return "", fmt.Errorf("sha256 mismatch: got %s want %s", digest, key)
	}
	if err := os.Rename(tmpName, cached); err != nil {
		return "", err
	}
	return cached, nil
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
	return json.NewDecoder(resp.Body).Decode(target)
}

func downloadTo(url string, tmp *os.File) error {
	client := &http.Client{Timeout: 30 * time.Second}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
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
	tmpDst := dst + ".envgen-tmp"
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
	fmt.Fprintf(os.Stderr, "envgen: "+format+"\n", args...)
	os.Exit(1)
}
