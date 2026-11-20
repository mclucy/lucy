//go:build ignore

// Command release-manifest builds the per-release manifest served as latest.json.
// Every archived asset is hashed locally, so metadata can only describe bytes
// that were actually downloaded.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const prefix = "release manifest:"

var (
	versionPattern  = regexp.MustCompile(`^(\d+\.\d+\.\d+)(-([0-9A-Za-z.-]+))?(\+[0-9A-Za-z.-]+)?$`)
	tagPattern      = regexp.MustCompile(`^v[0-9A-Za-z.+-]+$`)
	checksumPattern = regexp.MustCompile(`^([a-fA-F0-9]{64})\s+\*?(.+)$`)
)

type options struct {
	dir         string
	out         string
	tag         string
	version     string
	repo        string
	commit      string
	publishedAt string
}

type manifestFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Schema      int            `json:"schema"`
	Kind        string         `json:"kind"`
	Version     string         `json:"version"`
	Tag         string         `json:"tag"`
	Channel     string         `json:"channel"`
	Base        string         `json:"base"`
	Checksums   string         `json:"checksums"`
	Release     string         `json:"release"`
	Files       []manifestFile `json:"files"`
	Commit      string         `json:"commit,omitempty"`
	PublishedAt string         `json:"published_at,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s %v\n", prefix, err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	opts, err := parseArgs(argv)
	if err != nil {
		return err
	}
	if opts.dir == "" {
		return errors.New("missing --dir")
	}
	if opts.out == "" {
		return errors.New("missing --out")
	}
	if opts.tag == "" {
		return errors.New("missing --tag")
	}
	if opts.version == "" {
		return errors.New("missing --version")
	}
	if opts.repo == "" {
		opts.repo = os.Getenv("GITHUB_REPOSITORY")
	}
	if opts.repo == "" {
		opts.repo = "mclucy/lucy"
	}

	version := versionPattern.FindStringSubmatch(opts.version)
	if version == nil {
		return fmt.Errorf("invalid release version: %s", opts.version)
	}
	if !tagPattern.MatchString(opts.tag) {
		return fmt.Errorf("invalid release tag: %s", opts.tag)
	}
	channel := "stable"
	if version[2] != "" {
		channel = "prerelease"
	}

	digests, checksumNames, err := readChecksums(filepath.Join(opts.dir, "checksums.txt"))
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(opts.dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", opts.dir, err)
	}
	files := make([]manifestFile, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || entry.Name() == "checksums.txt" {
			continue
		}
		digest, named := digests[entry.Name()]
		if !named {
			return fmt.Errorf("checksums.txt does not name %s", entry.Name())
		}
		path := filepath.Join(opts.dir, entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s: %w", entry.Name(), err)
		}
		actual, err := sha256File(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", entry.Name(), err)
		}
		if actual != digest {
			return fmt.Errorf("checksum mismatch for %s", entry.Name())
		}
		files = append(files, manifestFile{Name: entry.Name(), Size: info.Size(), SHA256: digest})
		delete(digests, entry.Name())
	}

	var missing []string
	for _, name := range checksumNames {
		if _, ok := digests[name]; ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("checksums.txt names missing files: %s", strings.Join(missing, ", "))
	}
	if len(files) == 0 {
		return errors.New("no release assets found")
	}
	slices.SortFunc(files, func(a, b manifestFile) int {
		return strings.Compare(a.Name, b.Name)
	})

	result := manifest{
		Schema:      1,
		Kind:        "lucy-release",
		Version:     opts.version,
		Tag:         opts.tag,
		Channel:     channel,
		Base:        fmt.Sprintf("/dl/v%s/", opts.version),
		Checksums:   fmt.Sprintf("/dl/v%s/checksums.txt", opts.version),
		Release:     fmt.Sprintf("https://github.com/%s/releases/tag/%s", opts.repo, opts.tag),
		Files:       files,
		Commit:      opts.commit,
		PublishedAt: opts.publishedAt,
	}
	data, err := json.Marshal(result, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(opts.out), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(opts.out), err)
	}
	if err := os.WriteFile(opts.out, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", opts.out, err)
	}
	fmt.Printf("release manifest: %s (%s, %d files)\n", opts.out, channel, len(files))
	return nil
}

func parseArgs(argv []string) (options, error) {
	var opts options
	for i := 0; i < len(argv); i += 2 {
		key := argv[i]
		if !strings.HasPrefix(key, "--") || i+1 >= len(argv) {
			return opts, fmt.Errorf("invalid argument: %s", key)
		}
		value := argv[i+1]
		switch strings.TrimPrefix(key, "--") {
		case "dir":
			opts.dir = value
		case "out":
			opts.out = value
		case "tag":
			opts.tag = value
		case "version":
			opts.version = value
		case "repo":
			opts.repo = value
		case "commit":
			opts.commit = value
		case "published-at":
			opts.publishedAt = value
		}
	}
	return opts, nil
}

func readChecksums(path string) (map[string]string, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read checksums: %w", err)
	}
	digests := make(map[string]string)
	var names []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		match := checksumPattern.FindStringSubmatch(line)
		if match == nil {
			return nil, nil, fmt.Errorf("invalid checksum line: %s", line)
		}
		name := match[2]
		if _, duplicate := digests[name]; duplicate {
			return nil, nil, fmt.Errorf("duplicate checksum for %s", name)
		}
		digests[name] = strings.ToLower(match[1])
		names = append(names, name)
	}
	if len(digests) == 0 {
		return nil, nil, errors.New("checksums.txt is empty")
	}
	return digests, names, nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
