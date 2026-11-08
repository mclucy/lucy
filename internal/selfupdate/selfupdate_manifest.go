package selfupdate

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

const (
	manifestSchema  = 1
	maxManifestSize = 1 << 20
)

var versionPattern = regexp.MustCompile(
	`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`,
)

// Manifest is one release object served by the Worker as latest.json.
type Manifest struct {
	Schema      int    `json:"schema"`
	Kind        string `json:"kind"`
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Channel     string `json:"channel"`
	Commit      string `json:"commit"`
	PublishedAt string `json:"published_at"`
	Base        string `json:"base"`
	Checksums   string `json:"checksums"`
	Release     string `json:"release"`
	Files       []File `json:"files"`
}

// File is one downloadable release artifact.
type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type resolved struct {
	manifest        *Manifest
	base            *url.URL
	checksums       *url.URL
	file            File
	updateAvailable bool
}

func (s *Service) resolve(ctx context.Context, opts Options) (*resolved, error) {
	manifestURL := opts.ManifestURL
	if manifestURL == "" {
		manifestURL = DefaultManifestURL
	}

	manifest, manifestBase, base, err := s.fetchManifest(ctx, manifestURL)
	if err != nil {
		return nil, err
	}
	asset, err := AssetName(manifest.Version, s.goos, s.goarch)
	if err != nil {
		return nil, err
	}

	var selected File
	for _, file := range manifest.Files {
		if file.Name == asset {
			selected = file
			break
		}
	}
	if selected.Name == "" {
		return nil, fmt.Errorf(
			"release %s has no %s asset for %s/%s",
			manifest.Version,
			asset,
			s.goos,
			s.goarch,
		)
	}

	checksums, err := resolveReference(manifestBase, manifest.Checksums)
	if err != nil {
		return nil, err
	}
	if checksums == nil {
		checksums = base.JoinPath("checksums.txt")
	}
	if err := validateDownloadURL(base); err != nil {
		return nil, err
	}
	if err := validateDownloadURL(checksums); err != nil {
		return nil, err
	}

	update, err := updateAvailable(opts.Version, manifest.Version, opts.Force)
	if err != nil {
		return nil, err
	}
	return &resolved{
		manifest:        manifest,
		base:            base,
		checksums:       checksums,
		file:            selected,
		updateAvailable: update,
	}, nil
}

func (s *Service) fetchManifest(
	ctx context.Context,
	rawURL string,
) (*Manifest, *url.URL, *url.URL, error) {
	manifestURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse the release manifest URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build the release manifest request: %w", err)
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("user-agent", "lucy-self-update")
	s.logDebug("fetching release manifest", "url", rawURL)

	resp, err := s.doer.Do(req)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("fetch the release manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("fetch the release manifest: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifestSize+1))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read the release manifest: %w", err)
	}
	if len(data) > maxManifestSize {
		return nil, nil, nil, fmt.Errorf("release manifest exceeds %d bytes", maxManifestSize)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, nil, nil, fmt.Errorf("decode the release manifest: %w", err)
	}
	if manifest.Schema != manifestSchema {
		return nil, nil, nil, fmt.Errorf(
			"unsupported release manifest schema %d; update lucy with the install script",
			manifest.Schema,
		)
	}
	if kind := manifest.Kind; kind != "" && kind != "lucy-release" && kind != "lucy-channel" {
		return nil, nil, nil, fmt.Errorf("unsupported release manifest kind %q", kind)
	}
	if manifest.Version == "" {
		return nil, nil, nil, fmt.Errorf("release manifest has no version")
	}
	if !versionPattern.MatchString(manifest.Version) {
		return nil, nil, nil, fmt.Errorf("release manifest has an invalid version %q", manifest.Version)
	}
	if manifest.Tag != "" && strings.TrimPrefix(manifest.Tag, "v") != manifest.Version {
		return nil, nil, nil, fmt.Errorf(
			"release manifest tag %q does not match version %q",
			manifest.Tag,
			manifest.Version,
		)
	}
	if manifest.Base == "" {
		return nil, nil, nil, fmt.Errorf("release manifest has no base path")
	}

	base, err := resolveReference(manifestURL, manifest.Base)
	if err != nil {
		return nil, nil, nil, err
	}
	if base == nil {
		return nil, nil, nil, fmt.Errorf("release manifest has no base path")
	}
	return &manifest, manifestURL, base, nil
}

func resolveReference(base *url.URL, reference string) (*url.URL, error) {
	if reference == "" {
		return nil, nil
	}
	parsed, err := url.Parse(reference)
	if err != nil {
		return nil, fmt.Errorf("parse release path %q: %w", reference, err)
	}
	return base.ResolveReference(parsed), nil
}

// AssetName identifies the archive for one platform. The manifest must name
// the same file, so a release that misses a platform fails before download.
func AssetName(version, goos, goarch string) (string, error) {
	switch goos {
	case "linux", "darwin":
		if goarch != "amd64" && goarch != "arm64" {
			return "", fmt.Errorf("self-update is not supported on %s/%s", goos, goarch)
		}
		return fmt.Sprintf("lucy_%s_%s_%s.tar.gz", version, goos, goarch), nil
	case "windows":
		if goarch != "amd64" {
			return "", fmt.Errorf("self-update is not supported on %s/%s", goos, goarch)
		}
		return fmt.Sprintf("lucy_%s_windows_amd64.zip", version), nil
	default:
		return "", fmt.Errorf("self-update is not supported on %s/%s", goos, goarch)
	}
}

// validateDownloadURL keeps a manifest from redirecting the update to a
// non-transport scheme. Loopback HTTP is allowed for local mirrors and tests.
func validateDownloadURL(target *url.URL) error {
	if target == nil {
		return nil
	}
	if target.Scheme == "https" {
		return nil
	}
	if target.Scheme == "http" {
		switch strings.ToLower(target.Hostname()) {
		case "127.0.0.1", "::1", "localhost":
			return nil
		}
	}
	return fmt.Errorf("release manifest points to an unsupported download URL %q", target.String())
}

func updateAvailable(current, latest string, force bool) (bool, error) {
	if force {
		return true, nil
	}
	latestVersion, err := semver.NewVersion(latest)
	if err != nil {
		return false, fmt.Errorf("release manifest has an invalid version %q: %w", latest, err)
	}
	current = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(current), "v"))
	if current == "" {
		return true, nil
	}
	currentVersion, err := semver.NewVersion(current)
	if err != nil {
		// A development stamp that is not valid semver cannot be compared;
		// offer the release and let the installation check decide.
		return true, nil
	}
	return latestVersion.GreaterThan(currentVersion), nil
}
