// Package importpack reads external server modpack sources into a staging tree.
package importpack

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/server"
)

// Format identifies the source layout detected from archive contents.
type Format string

const (
	FormatMrpack Format = "mrpack"
	FormatTree   Format = "tree"
)

// Plan describes a source before or after materialization.
//
// ServerFiles counts the files written to the destination, Overrides counts
// those taken from override folders, and Bytes sums their sizes. Tree sources
// leave Minecraft, Product, and CoreVersion empty; discovery determines them
// after materialization.
type Plan struct {
	Format        Format
	Name          string
	VersionID     string
	Minecraft     string
	Product       server.Product
	CoreVersion   string
	ServerFiles   int
	SkippedClient int
	Overrides     int
	Bytes         int64
}

// Result reports what Materialize wrote and what it intentionally skipped.
// Skipped lists the source paths of client-only entries that were not written.
type Result struct {
	Plan
	Skipped []string
}

// Progress receives facts about the downloads Materialize performs. Every hook
// is optional: a nil hook reports nothing. ReportDone runs after the file is
// verified and written, fromCache when no bytes crossed the network.
type Progress struct {
	FileStarted  func(name string, size int64)
	FileProgress func(name string, read, total int64)
	FileDone     func(name string, fromCache bool)
}

func (p Progress) ReportStarted(name string, size int64) {
	if p.FileStarted != nil {
		p.FileStarted(name, size)
	}
}

func (p Progress) ReportProgress(name string, read, total int64) {
	if p.FileProgress != nil {
		p.FileProgress(name, read, total)
	}
}

func (p Progress) ReportDone(name string, fromCache bool) {
	if p.FileDone != nil {
		p.FileDone(name, fromCache)
	}
}

// FormatError reports an archive whose contents are not a supported server
// source. Reason is user-facing and should name the observed format.
type FormatError struct {
	Kind   string
	Reason string
}

func (e *FormatError) Error() string {
	if e.Kind == "" {
		return e.Reason
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Reason)
}

// Inspect detects the source format and reads declarative metadata without
// writing or downloading anything.
func Inspect(ctx context.Context, path string) (*Plan, error) {
	return inspect(ctx, path)
}

// Materialize writes the server-side contents of path into dest. Unix file
// modes are sanitized; symlinks, devices, and paths escaping dest are
// rejected. network may be nil only when the source needs no downloads.
// progress may be zero; its hooks stay silent.
func Materialize(
	ctx context.Context,
	path, dest string,
	network *cache.Client,
	progress Progress,
) (*Result, error) {
	return materialize(ctx, path, dest, network, progress)
}

const (
	kindUnsupportedArchive = "unsupported archive"
	kindClientPack         = "client pack"
	kindLauncherInstance   = "launcher instance"
	kindWorldOnly          = "world-only archive"
	kindUnrecognized       = "unrecognized source"
	kindInvalidModpack     = "invalid modpack"
)

// Fixed import limits. A source beyond one of them is rejected before any
// file past the limit is written.
const (
	// MaxSourceBytes is the largest archive an import accepts.
	MaxSourceBytes    int64 = 4 << 30
	maxExtractedBytes int64 = 8 << 30
	maxFileBytes      int64 = 2 << 30
	maxEntries              = 50000
	maxPathBytes            = 1024
	maxPathDepth            = 32
	maxMetadataBytes  int64 = 16 << 20
)

var (
	// ErrUnsafeEntry reports an archive path or entry type that could escape
	// the destination or create a link.
	ErrUnsafeEntry = errors.New("unsafe archive entry")
	// ErrLimitExceeded reports a source beyond a fixed import limit.
	ErrLimitExceeded = errors.New("import limit exceeded")
)

// Layers rank sources of the same destination path. A higher layer replaces a
// lower layer's file; within one layer a path may appear only once.
const (
	layerBase = iota
	layerOverrides
	layerServerOverrides
)

// item is one directory or file planned for the destination.
type item struct {
	rel      string
	dir      bool
	implicit bool
	mode     fs.FileMode
	size     int64
	layer    int
	ordinal  int
	download *mrpackFile
}

// layout is a validated, ordered plan ready for materialization.
type layout struct {
	plan      Plan
	items     []*item
	skipped   []string
	downloads int
}

func newLayout(items []*item, skipped []string) (*layout, error) {
	lay := &layout{items: items, skipped: skipped}
	for _, it := range items {
		if it.dir {
			continue
		}
		if it.size > maxFileBytes {
			return nil, limitError(
				"%s is %d bytes, above the %d byte file limit",
				it.rel,
				it.size,
				maxFileBytes,
			)
		}
		lay.plan.ServerFiles++
		lay.plan.Bytes += it.size
		if it.layer != layerBase {
			lay.plan.Overrides++
		}
		if it.download != nil {
			lay.downloads++
		}
	}
	if lay.plan.Bytes > maxExtractedBytes {
		return nil, limitError(
			"server files total %d bytes, above the %d byte limit",
			lay.plan.Bytes,
			maxExtractedBytes,
		)
	}
	lay.plan.SkippedClient = len(skipped)
	return lay, nil
}

// pathTable holds every planned destination path, keyed by its case-folded
// form, so that case-only collisions and file/directory conflicts are caught.
type pathTable struct {
	byKey map[string]*item
}

func newPathTable() *pathTable {
	return &pathTable{byKey: map[string]*item{}}
}

// add places it in the table. It reports superseded when a higher layer
// already holds a file at the same path, in which case nothing is recorded.
func (t *pathTable) add(it *item) (bool, error) {
	if err := checkDestPath(it.rel); err != nil {
		return false, err
	}
	parts := strings.Split(it.rel, "/")
	for i := 1; i < len(parts); i++ {
		if err := t.addAncestor(strings.Join(parts[:i], "/"), it.layer); err != nil {
			return false, err
		}
	}
	key := foldKey(it.rel)
	existing, ok := t.byKey[key]
	if !ok {
		t.byKey[key] = it
		return false, nil
	}
	if existing.rel != it.rel {
		return false, collision(it.rel, existing.rel)
	}
	switch {
	case existing.dir && it.dir:
		if existing.implicit {
			t.byKey[key] = it
		}
		return false, nil
	case !existing.dir && !it.dir && existing.layer > it.layer:
		return true, nil
	case !existing.dir && !it.dir:
		return false, fmt.Errorf("%w: %q appears more than once", ErrUnsafeEntry, it.rel)
	}
	return false, fmt.Errorf("%w: %q is both a file and a directory", ErrUnsafeEntry, it.rel)
}

func (t *pathTable) addAncestor(rel string, layer int) error {
	key := foldKey(rel)
	existing, ok := t.byKey[key]
	if !ok {
		t.byKey[key] = &item{
			rel:      rel,
			dir:      true,
			implicit: true,
			mode:     dirMode,
			layer:    layer,
			ordinal:  -1,
		}
		return nil
	}
	if existing.rel != rel {
		return collision(rel, existing.rel)
	}
	if !existing.dir {
		return fmt.Errorf("%w: %q is both a file and a directory", ErrUnsafeEntry, rel)
	}
	return nil
}

// items returns the explicit directories and files in path order. Parent
// directories that no entry names explicitly are created on demand.
func (t *pathTable) items() []*item {
	var out []*item
	for _, it := range t.byKey {
		if !it.implicit {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out
}

func foldKey(rel string) string {
	return strings.ToLower(rel)
}

func collision(rel, other string) error {
	return fmt.Errorf("%w: %q collides with %q", ErrUnsafeEntry, rel, other)
}

// checkDestPath validates a normalized, slash-separated destination path.
func checkDestPath(rel string) error {
	if len(rel) > maxPathBytes {
		return limitError("path is longer than %d bytes: %q", maxPathBytes, rel)
	}
	if depth := strings.Count(rel, "/") + 1; depth > maxPathDepth {
		return limitError("path %q is %d levels deep, above the %d level limit", rel, depth, maxPathDepth)
	}
	first, _, _ := strings.Cut(rel, "/")
	if strings.EqualFold(first, fsutil.WorkspaceMarker) {
		return fmt.Errorf("%w: %q targets reserved workspace metadata", ErrUnsafeEntry, rel)
	}
	if !strings.Contains(rel, "/") &&
		(strings.EqualFold(rel, document.ManifestFilename) ||
			strings.EqualFold(rel, document.LockFilename)) {
		return fmt.Errorf(
			"%w: %q is a Lucy workspace document; import the tree without it",
			ErrUnsafeEntry,
			rel,
		)
	}
	if err := document.SafeRelativePath(filepath.FromSlash(rel)); err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeEntry, err)
	}
	return nil
}

func parentRel(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return ""
}

func newFormatError(kind, format string, args ...any) *FormatError {
	return &FormatError{Kind: kind, Reason: fmt.Sprintf(format, args...)}
}

func limitError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrLimitExceeded, fmt.Sprintf(format, args...))
}
