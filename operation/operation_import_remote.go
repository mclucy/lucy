package operation

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"unicode"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/importpack"
	"github.com/mclucy/lucy/upstream"
)

// importPackResolver is the optional capability a Source provides to turn a
// remote modpack reference into the pack file its provider publishes. A Source
// without it cannot import remote modpacks.
type importPackResolver interface {
	ResolveImportPack(
		ctx context.Context,
		provider catalog.Provider,
		project, version string,
	) (upstream.ImportPack, error)
}

// RemoteImportSource names a modpack on a provider. Project is a slug or, for
// CurseForge, a numeric id. Version is empty when the source does not pin a
// version or file, so the resolver chooses the provider's latest pack.
type RemoteImportSource struct {
	Provider catalog.Provider
	Project  string
	Version  string
}

const (
	modrinthIdentifierPrefix   = "modrinth:"
	curseforgeIdentifierPrefix = "curseforge:"
)

// ParseRemoteImportSource recognizes provider identifiers and modpack project
// pages. ok is false for every other source, which keeps local paths and direct
// archive URLs on their existing path. A source that is recognized but
// malformed reports ok with an error, so it fails before any file is touched.
func ParseRemoteImportSource(source string) (RemoteImportSource, bool, error) {
	lower := strings.ToLower(source)
	switch {
	case strings.HasPrefix(lower, modrinthIdentifierPrefix):
		return parseModrinthIdentifier(source[len(modrinthIdentifierPrefix):])
	case strings.HasPrefix(lower, curseforgeIdentifierPrefix):
		return parseCurseForgeIdentifier(source[len(curseforgeIdentifierPrefix):])
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" {
		return RemoteImportSource{}, false, nil
	}
	segments := strings.FieldsFunc(parsed.Path, func(r rune) bool { return r == '/' })
	switch strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.") {
	case "modrinth.com":
		if len(segments) == 0 || segments[0] != "modpack" {
			return RemoteImportSource{}, false, nil
		}
		ref, err := parseModrinthPage(source, segments[1:])
		return ref, true, err
	case "curseforge.com":
		if len(segments) < 2 || segments[0] != "minecraft" || segments[1] != "modpacks" {
			return RemoteImportSource{}, false, nil
		}
		ref, err := parseCurseForgePage(source, segments[2:])
		return ref, true, err
	}
	return RemoteImportSource{}, false, nil
}

// parseModrinthIdentifier reads the body of modrinth:<slug>[@<version>].
func parseModrinthIdentifier(body string) (RemoteImportSource, bool, error) {
	project, version, pinned := strings.Cut(body, "@")
	if !validRemoteName(project) || (pinned && !validRemoteName(version)) {
		return RemoteImportSource{}, true, fmt.Errorf(
			"invalid Modrinth source %q: want modrinth:<slug>[@<version>]",
			modrinthIdentifierPrefix+body,
		)
	}
	return RemoteImportSource{
		Provider: catalog.ProviderModrinth,
		Project:  project,
		Version:  version,
	}, true, nil
}

// parseCurseForgeIdentifier reads the body of curseforge:<slug-or-id>[@<file-id>].
func parseCurseForgeIdentifier(body string) (RemoteImportSource, bool, error) {
	project, file, pinned := strings.Cut(body, "@")
	if !validRemoteName(project) || (pinned && !isDecimal(file)) {
		return RemoteImportSource{}, true, fmt.Errorf(
			"invalid CurseForge source %q: want curseforge:<slug-or-id>[@<file-id>] with a numeric file id",
			curseforgeIdentifierPrefix+body,
		)
	}
	return RemoteImportSource{
		Provider: catalog.ProviderCurseForge,
		Project:  project,
		Version:  file,
	}, true, nil
}

// parseModrinthPage reads the path after /modpack/.
func parseModrinthPage(source string, rest []string) (RemoteImportSource, error) {
	switch {
	case len(rest) == 1 && validRemoteName(rest[0]):
		return RemoteImportSource{Provider: catalog.ProviderModrinth, Project: rest[0]}, nil
	case len(rest) == 3 && rest[1] == "version" &&
		validRemoteName(rest[0]) && validRemoteName(rest[2]):
		return RemoteImportSource{
			Provider: catalog.ProviderModrinth,
			Project:  rest[0],
			Version:  rest[2],
		}, nil
	}
	return RemoteImportSource{}, fmt.Errorf(
		"unsupported Modrinth modpack URL %q: want https://modrinth.com/modpack/<slug>[/version/<version>]",
		source,
	)
}

// parseCurseForgePage reads the path after /minecraft/modpacks/.
func parseCurseForgePage(source string, rest []string) (RemoteImportSource, error) {
	switch {
	case len(rest) == 1 && validRemoteName(rest[0]):
		return RemoteImportSource{Provider: catalog.ProviderCurseForge, Project: rest[0]}, nil
	case len(rest) == 3 && rest[1] == "files" &&
		validRemoteName(rest[0]) && isDecimal(rest[2]):
		return RemoteImportSource{
			Provider: catalog.ProviderCurseForge,
			Project:  rest[0],
			Version:  rest[2],
		}, nil
	}
	return RemoteImportSource{}, fmt.Errorf(
		"unsupported CurseForge modpack URL %q: want https://www.curseforge.com/minecraft/modpacks/<slug>[/files/<file-id>]",
		source,
	)
}

// validRemoteName reports whether name can be a slug, id or version segment
// without separators, spaces or query characters.
func validRemoteName(name string) bool {
	return name != "" && !strings.ContainsFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune(`/\@?#`, r)
	})
}

func isDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// resolveImportSource returns the local archive path for source. Remote modpack
// references are resolved and downloaded; every other source keeps the
// local-path and direct-URL handling of acquireImportSource.
func (s *Service) resolveImportSource(
	ctx context.Context,
	source, sha256 string,
	network *cache.Client,
) (string, string, func(), error) {
	ref, ok, err := ParseRemoteImportSource(source)
	if err != nil {
		return "", "", nil, err
	}
	if !ok {
		return acquireImportSource(ctx, source, sha256, network)
	}
	return s.acquireRemoteImportSource(ctx, source, ref, sha256, network)
}

// acquireRemoteImportSource asks the configured resolver for the pack a remote
// reference names and downloads it. The returned label is the user's source.
func (s *Service) acquireRemoteImportSource(
	ctx context.Context,
	source string,
	ref RemoteImportSource,
	sha256 string,
	network *cache.Client,
) (string, string, func(), error) {
	resolver, ok := s.source.(importPackResolver)
	if !ok {
		return "", "", nil, fmt.Errorf(
			"import source %q: no %s pack resolver is configured",
			source,
			ref.Provider,
		)
	}
	if network == nil {
		return "", "", nil, fmt.Errorf("import source %q requires a network client", source)
	}
	pack, err := resolver.ResolveImportPack(ctx, ref.Provider, ref.Project, ref.Version)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve import source %q: %w", source, err)
	}
	return downloadImportPack(ctx, source, pack, sha256, network)
}

// downloadImportPack fetches a resolved pack into a private temporary directory
// through the bounded source download. The cache verifies the provider's hash,
// preferring SHA-512 over SHA-1, and --sha256 is checked on the downloaded bytes.
func downloadImportPack(
	ctx context.Context,
	source string,
	pack upstream.ImportPack,
	sha256 string,
	network *cache.Client,
) (string, string, func(), error) {
	rawURL := strings.TrimSpace(pack.URL)
	if rawURL == "" {
		return "", "", nil, fmt.Errorf("import source %q: resolved pack has no download URL", source)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", nil, fmt.Errorf("import source %q: parse resolved pack URL: %w", source, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", nil, fmt.Errorf(
			"import source %q: resolved pack URL scheme %q is not http or https",
			source,
			parsed.Scheme,
		)
	}
	if parsed.Host == "" {
		return "", "", nil, fmt.Errorf("import source %q: resolved pack URL has no host", source)
	}
	name := strings.TrimSpace(pack.Filename)
	if name == "" {
		return "", "", nil, fmt.Errorf("import source %q: resolved pack has no filename", source)
	}
	name = path.Base(name)
	if name == "." || name == ".." || name == "/" {
		return "", "", nil, fmt.Errorf(
			"import source %q: resolved pack filename %q is not a file name",
			source,
			pack.Filename,
		)
	}

	dir, err := os.MkdirTemp("", "lucy-import-pack-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create import download directory: %w", err)
	}
	release := func() { _ = os.RemoveAll(dir) }
	algorithm, digest := importPackDigest(pack.Hashes)
	download, err := network.CachedDownload(ctx, rawURL, dir, cache.DownloadOptions{
		Kind:          cache.KindArtifact,
		ExpectedHash:  digest,
		HashAlgorithm: algorithm,
		Filename:      name,
		FileMode:      0o600,
		MaxBytes:      importpack.MaxSourceBytes,
	})
	if err != nil {
		release()
		return "", "", nil, fmt.Errorf("download resolved pack for %q: %w", source, err)
	}
	file := download.File.Name()
	if err := download.File.Close(); err != nil {
		release()
		return "", "", nil, err
	}
	if err := verifyImportSHA256(file, sha256); err != nil {
		release()
		return "", "", nil, err
	}
	return file, source, release, nil
}

// importPackDigest selects the strongest digest a pack publishes. An absent
// digest yields HashNone, which the cache treats as no expectation.
func importPackDigest(hashes map[string]string) (cache.HashAlgorithm, string) {
	candidates := []struct {
		name      string
		algorithm cache.HashAlgorithm
	}{
		{name: "sha512", algorithm: cache.HashSHA512},
		{name: "sha1", algorithm: cache.HashSHA1},
	}
	for _, candidate := range candidates {
		if digest := strings.ToLower(strings.TrimSpace(hashes[candidate.name])); digest != "" {
			return candidate.algorithm, digest
		}
	}
	return cache.HashNone, ""
}
