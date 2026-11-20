package importpack

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/server"
)

const modrinthIndexName = "modrinth.index.json"

const (
	mrpackOverridesPrefix       = "overrides/"
	mrpackServerOverridesPrefix = "server-overrides/"
	mrpackClientOverridesPrefix = "client-overrides/"
)

// mrpackOverrideSources lists override folders from highest to lowest priority,
// so a lower-priority file at the same path is dropped before it is written or
// downloaded.
var mrpackOverrideSources = []struct {
	prefix string
	layer  int
}{
	{prefix: mrpackServerOverridesPrefix, layer: layerServerOverrides},
	{prefix: mrpackOverridesPrefix, layer: layerOverrides},
}

// mrpackLoaderProducts maps a Modrinth dependency key to a server product name.
var mrpackLoaderProducts = map[string]string{
	"fabric-loader": "fabric",
	"quilt-loader":  "quilt",
	"forge":         "forge",
	"neoforge":      "neoforge",
}

type mrpackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Files         []mrpackIndexFile `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
}

type mrpackIndexFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       map[string]string `json:"env"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

// mrpackFile is a download the index declares for one destination path.
type mrpackFile struct {
	urls      []string
	algorithm cache.HashAlgorithm
	digest    string
}

func parseMrpackIndex(data []byte) (*mrpackIndex, error) {
	var index mrpackIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, newFormatError(kindInvalidModpack, "%s is not valid JSON: %v", modrinthIndexName, err)
	}
	if index.FormatVersion != 1 {
		return nil, newFormatError(
			kindInvalidModpack,
			"unsupported %s formatVersion %d; lucy reads version 1",
			modrinthIndexName,
			index.FormatVersion,
		)
	}
	if index.Game != "minecraft" {
		return nil, newFormatError(kindInvalidModpack, "game %q is not minecraft", index.Game)
	}
	if len(index.Files) > maxEntries {
		return nil, limitError("%s lists %d files, above the %d entry limit", modrinthIndexName, len(index.Files), maxEntries)
	}
	return &index, nil
}

// mapDependencies converts the Modrinth dependency map to a Minecraft version
// and a server product. Without a loader the product is vanilla Minecraft,
// whose core version is the Minecraft version itself.
func mapDependencies(deps map[string]string) (string, server.Product, string, error) {
	minecraft := strings.TrimSpace(deps["minecraft"])
	if minecraft == "" {
		return "", server.ProductUnknown, "", newFormatError(
			kindInvalidModpack,
			"dependencies do not name a minecraft version",
		)
	}
	var loaders []string
	for key := range deps {
		if key != "minecraft" {
			loaders = append(loaders, key)
		}
	}
	sort.Strings(loaders)
	switch len(loaders) {
	case 0:
		return minecraft, server.ProductMinecraft, minecraft, nil
	case 1:
	default:
		return "", server.ProductUnknown, "", newFormatError(
			kindInvalidModpack,
			"dependencies name several loaders (%s); a server runs one loader",
			strings.Join(loaders, ", "),
		)
	}
	loader := loaders[0]
	productName, ok := mrpackLoaderProducts[loader]
	if !ok {
		return "", server.ProductUnknown, "", newFormatError(
			kindInvalidModpack,
			"unsupported loader %q; supported loaders are fabric-loader, quilt-loader, forge, and neoforge",
			loader,
		)
	}
	product, known := server.ParseProduct(productName)
	if !known {
		return "", server.ProductUnknown, "", newFormatError(
			kindInvalidModpack,
			"loader %q is not a known server product",
			loader,
		)
	}
	core := strings.TrimSpace(deps[loader])
	if core == "" {
		return "", server.ProductUnknown, "", newFormatError(
			kindInvalidModpack,
			"loader %q has no version",
			loader,
		)
	}
	return minecraft, product, core, nil
}

// serverIncluded applies env.server: a missing value includes the file,
// "unsupported" excludes it, and any other value is rejected.
func serverIncluded(env map[string]string) (bool, error) {
	switch env["server"] {
	case "", "required", "optional":
		return true, nil
	case "unsupported":
		return false, nil
	default:
		return false, newFormatError(
			kindInvalidModpack,
			"unknown env.server value %q",
			env["server"],
		)
	}
}

func normalizeIndexPath(raw string) (string, error) {
	if strings.HasSuffix(raw, "/") {
		return "", newFormatError(kindInvalidModpack, "file path %q names a directory", raw)
	}
	rel, err := normalizeEntryName(raw)
	if err != nil {
		return "", fmt.Errorf("file path in %s: %w", modrinthIndexName, err)
	}
	if rel == "" {
		return "", newFormatError(kindInvalidModpack, "file path %q is empty", raw)
	}
	if err := checkDestPath(rel); err != nil {
		return "", err
	}
	return rel, nil
}

// selectDigest prefers sha512 and falls back to sha1. A file without either
// cannot be verified, so it is rejected.
func selectDigest(rel string, hashes map[string]string) (cache.HashAlgorithm, string, error) {
	candidates := []struct {
		name      string
		algorithm cache.HashAlgorithm
		length    int
	}{
		{name: "sha512", algorithm: cache.HashSHA512, length: sha512.Size * 2},
		{name: "sha1", algorithm: cache.HashSHA1, length: sha1.Size * 2},
	}
	for _, c := range candidates {
		digest := strings.ToLower(strings.TrimSpace(hashes[c.name]))
		if digest == "" {
			continue
		}
		if len(digest) != c.length {
			return cache.HashNone, "", newFormatError(
				kindInvalidModpack,
				"%s hash for %s has %d hex digits, want %d",
				c.name,
				rel,
				len(digest),
				c.length,
			)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return cache.HashNone, "", newFormatError(
				kindInvalidModpack,
				"%s hash for %s is not hex",
				c.name,
				rel,
			)
		}
		return c.algorithm, digest, nil
	}
	return cache.HashNone, "", newFormatError(
		kindInvalidModpack,
		"%s has no sha512 or sha1 hash to verify it against",
		rel,
	)
}

// buildMrpackLayout plans a Modrinth modpack. Override folders take precedence
// over index downloads, and client-only entries are reported as skipped.
func buildMrpackLayout(a *archive) (*layout, error) {
	indexEntry, _ := a.rootFile(modrinthIndexName)
	data, err := a.readEntry(indexEntry, maxMetadataBytes)
	if err != nil {
		return nil, err
	}
	index, err := parseMrpackIndex(data)
	if err != nil {
		return nil, err
	}
	minecraft, product, core, err := mapDependencies(index.Dependencies)
	if err != nil {
		return nil, err
	}

	table := newPathTable()
	var skipped []string
	for _, source := range mrpackOverrideSources {
		for _, e := range a.entries {
			rel, ok := strings.CutPrefix(e.rel, source.prefix)
			if !ok || rel == "" {
				continue
			}
			if _, err := table.add(entryItem(e, rel, source.layer)); err != nil {
				return nil, err
			}
		}
	}
	for _, e := range a.entries {
		if !e.dir && strings.HasPrefix(e.rel, mrpackClientOverridesPrefix) {
			skipped = append(skipped, e.rel)
		}
	}

	for _, f := range index.Files {
		rel, err := normalizeIndexPath(f.Path)
		if err != nil {
			return nil, err
		}
		included, err := serverIncluded(f.Env)
		if err != nil {
			return nil, err
		}
		if !included {
			skipped = append(skipped, rel)
			continue
		}
		if len(f.Downloads) == 0 {
			return nil, newFormatError(kindInvalidModpack, "%s has no download URL", rel)
		}
		if f.FileSize < 0 {
			return nil, newFormatError(kindInvalidModpack, "%s has a negative fileSize", rel)
		}
		algorithm, digest, err := selectDigest(rel, f.Hashes)
		if err != nil {
			return nil, err
		}
		it := &item{
			rel:     rel,
			mode:    fileMode(false),
			size:    f.FileSize,
			layer:   layerBase,
			ordinal: -1,
			download: &mrpackFile{
				urls:      f.Downloads,
				algorithm: algorithm,
				digest:    digest,
			},
		}
		if _, err := table.add(it); err != nil {
			return nil, err
		}
	}

	lay, err := newLayout(table.items(), skipped)
	if err != nil {
		return nil, err
	}
	if lay.plan.ServerFiles == 0 {
		return nil, newFormatError(kindInvalidModpack, "%s contains no server files", filepath.Base(a.path))
	}
	lay.plan.Format = FormatMrpack
	lay.plan.Name = index.Name
	lay.plan.VersionID = index.VersionID
	lay.plan.Minecraft = minecraft
	lay.plan.Product = product
	lay.plan.CoreVersion = core
	return lay, nil
}

// fetch downloads one index file into a private staging directory, verifies
// it, and renames it to its destination. Each URL is tried in order until one
// yields a verified file. The per-file stage directory keeps files that share
// a base name from writing the same staging path.
func (m *materializer) fetch(ctx context.Context, it *item, index int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Join(m.stage, strconv.Itoa(index))
	if err := os.Mkdir(dir, dirMode); err != nil {
		return fmt.Errorf("prepare stage for %s: %w", it.rel, err)
	}
	var failures []error
	for _, url := range it.download.urls {
		if err := ctx.Err(); err != nil {
			return err
		}
		staged, err := m.downloadFrom(ctx, url, it, dir)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", url, err))
			continue
		}
		rel, err := filepath.Rel(m.dest, staged)
		if err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("place %s: %w", it.rel, ErrUnsafeEntry)
		}
		if err := m.root.Rename(rel, filepath.FromSlash(it.rel)); err != nil {
			return fmt.Errorf("place %s: %w", it.rel, err)
		}
		return nil
	}
	return fmt.Errorf("download %s: no source produced a verified file: %w", it.rel, errors.Join(failures...))
}

// downloadFrom fetches url into dir and returns the staged file's path.
func (m *materializer) downloadFrom(
	ctx context.Context,
	url string,
	it *item,
	dir string,
) (string, error) {
	digest := it.download
	name := path.Base(it.rel)
	// Bound one transfer by the per-file limit and by the bytes left after
	// files already retained. The aggregate is charged only on retention, so a
	// failing attempt in flight cannot shrink another attempt's limit.
	limit := min(maxFileBytes, m.budget.remaining())
	if limit <= 0 {
		return "", limitError("extraction exceeds the %d byte limit", maxExtractedBytes)
	}
	res, err := m.network.CachedDownload(ctx, url, dir, cache.DownloadOptions{
		Kind:          cache.KindArtifact,
		ExpectedHash:  digest.digest,
		HashAlgorithm: digest.algorithm,
		Filename:      name,
		FileMode:      0o644,
		MaxBytes:      limit,
		WrapReader: func(r io.Reader, size int64) io.Reader {
			m.reportStarted(it.rel, size)
			return cache.ProgressReader(r, size, func(read, total int64) {
				m.reportProgress(it.rel, read, total)
			})
		},
	})
	if err != nil {
		// The cache cannot tell its per-file bound from the aggregate one; when
		// the aggregate shrank the limit, report the shared import limit.
		if limit < maxFileBytes && errors.Is(err, cache.ErrDownloadTooLarge) {
			return "", fmt.Errorf("%w: %w", limitError("extraction exceeds the %d byte limit", maxExtractedBytes), err)
		}
		return "", err
	}
	staged := res.File.Name()
	closeErr := res.File.Close()
	if closeErr == nil {
		closeErr = m.retainStage(staged, it.rel, digest, res.Verified)
	}
	if closeErr != nil {
		_ = os.Remove(staged)
		return "", closeErr
	}
	m.reportDone(it.rel, res.CacheHit)
	return staged, nil
}

// retainStage charges a staged download to the aggregate budget before it can
// be renamed into place. Hosted bytes the cache did not verify are checked
// here, because the cache checks digests only when it stores an entry.
func (m *materializer) retainStage(
	staged, rel string,
	digest *mrpackFile,
	verified bool,
) error {
	info, err := os.Stat(staged)
	if err != nil {
		return err
	}
	if info.Size() > maxFileBytes {
		return limitError("%s is %d bytes, above the %d byte file limit", rel, info.Size(), maxFileBytes)
	}
	if !verified {
		if err := verifyDigest(staged, digest.algorithm, digest.digest); err != nil {
			return err
		}
	}
	if err := m.budget.spend(info.Size()); err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return nil
}

func verifyDigest(file string, algorithm cache.HashAlgorithm, expected string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	var h hash.Hash
	switch algorithm {
	case cache.HashSHA512:
		h = sha512.New()
	case cache.HashSHA1:
		h = sha1.New()
	default:
		return fmt.Errorf("unsupported digest algorithm %s", algorithm)
	}
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if actual := hex.EncodeToString(h.Sum(nil)); actual != expected {
		return fmt.Errorf("%s mismatch: expected %s, got %s", algorithm, expected, actual)
	}
	return nil
}
