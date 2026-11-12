package importpack

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/fsutil"
)

const dirMode fs.FileMode = 0o755

type containerKind uint8

const (
	containerZip containerKind = iota + 1
	containerTarGzip
)

func (k containerKind) String() string {
	if k == containerZip {
		return "zip"
	}
	return "tar.gz"
}

// archive is an opened source container whose entries passed the safety and
// limit checks of scan.
type archive struct {
	path      string
	stem      string
	container containerKind
	mrpack    bool
	file      *os.File
	zip       *zip.Reader
	entries   []entry
	// budget bounds the body bytes a tar scan may drain before it rejects.
	budget int64
}

// entry is one file or directory of a source. name keeps any wrapper
// directory; rel is the path relative to the source root once the wrapper is
// removed. Entries that name only the wrapper itself are dropped.
type entry struct {
	ordinal int
	name    string
	rel     string
	dir     bool
	size    int64
	exec    bool
}

// visited is one container header as read by visit, before wrapper removal.
type visited struct {
	name string
	dir  bool
	size int64
	exec bool
}

type visitFunc func(ordinal int, v visited, body io.Reader) error

// serverRootFiles and serverRootDirs mark a directory as a server installation
// when they appear at the source root.
var serverRootFiles = map[string]bool{
	"server.properties":   true,
	"eula.txt":            true,
	"ops.json":            true,
	"whitelist.json":      true,
	"banned-ips.json":     true,
	"banned-players.json": true,
	"usercache.json":      true,
	"bukkit.yml":          true,
	"spigot.yml":          true,
	"paper-global.yml":    true,
	"server-icon.png":     true,
	"user_jvm_args.txt":   true,
	"start.sh":            true,
	"start.bat":           true,
	"run.sh":              true,
	"run.bat":             true,
}

var serverRootDirs = []string{"mods", "plugins", "config", "libraries"}

func inspect(ctx context.Context, path string) (*Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, err := openArchive(ctx, path)
	if err != nil {
		return nil, err
	}
	defer a.file.Close()
	lay, err := buildLayout(a)
	if err != nil {
		return nil, err
	}
	plan := lay.plan
	return &plan, nil
}

func materialize(
	ctx context.Context,
	path, dest string,
	network *cache.Client,
	progress Progress,
) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, err := openArchive(ctx, path)
	if err != nil {
		return nil, err
	}
	defer a.file.Close()
	lay, err := buildLayout(a)
	if err != nil {
		return nil, err
	}
	if lay.downloads > 0 && network == nil {
		return nil, fmt.Errorf(
			"%s needs %d downloads, but no network client was provided",
			filepath.Base(path),
			lay.downloads,
		)
	}
	m, err := newMaterializer(ctx, dest, network, progress)
	if err != nil {
		return nil, err
	}
	defer m.cleanup()
	if err := m.run(a, lay); err != nil {
		return nil, err
	}
	plan := lay.plan
	plan.Bytes = m.budget.used
	return &Result{Plan: plan, Skipped: lay.skipped}, nil
}

// openArchive checks the extension, the content signature, and the source size
// before scanning entries. The caller must close the returned archive's file.
func openArchive(ctx context.Context, path string) (*archive, error) {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	a := &archive{path: path, budget: maxExtractedBytes}
	var suffix string
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		a.container, suffix = containerTarGzip, ".tar.gz"
	case strings.HasSuffix(lower, ".mrpack"):
		a.container, suffix, a.mrpack = containerZip, ".mrpack", true
	case strings.HasSuffix(lower, ".zip"):
		a.container, suffix = containerZip, ".zip"
	default:
		container, err := sniffContainer(path)
		if err != nil {
			return nil, err
		}
		a.container = container
		suffix = ""
	}
	a.stem = base[:len(base)-len(suffix)]
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	a.file = file
	if err := a.open(ctx); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return a, nil
}

func (a *archive) open(ctx context.Context) error {
	info, err := a.file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", a.path)
	}
	if info.Size() > MaxSourceBytes {
		return limitError(
			"%s is %d bytes, above the %d byte source limit",
			a.path,
			info.Size(),
			MaxSourceBytes,
		)
	}
	magic := make([]byte, 4)
	n, _ := io.ReadFull(a.file, magic)
	if !a.matchesMagic(magic[:n]) {
		return newFormatError(
			kindUnsupportedArchive,
			"%s has a %s extension but its contents are not a %s archive",
			filepath.Base(a.path),
			a.container,
			a.container,
		)
	}
	if _, err := a.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if a.container == containerZip {
		// The archive reader allocates one structure per entry the central
		// directory declares, so the entry limit applies before it parses.
		if entries, ok := zipDirectoryEntries(a.file, info.Size()); ok &&
			entries > uint64(maxEntries) {
			return limitError("archive has more than %d entries", maxEntries)
		}
		reader, err := zip.NewReader(a.file, info.Size())
		if err != nil {
			return fmt.Errorf("read zip archive %s: %w", a.path, err)
		}
		a.zip = reader
	}
	return a.scan(ctx)
}

func (a *archive) matchesMagic(magic []byte) bool {
	switch a.container {
	case containerZip:
		return bytes.HasPrefix(magic, []byte("PK\x03\x04")) ||
			bytes.HasPrefix(magic, []byte("PK\x05\x06"))
	case containerTarGzip:
		return bytes.HasPrefix(magic, []byte{0x1f, 0x8b})
	}
	return false
}

// zipDirectoryEntries reports how many entries a zip central directory
// declares. The end record holds the count unless the archive is too large for
// it, in which case the ZIP64 record holds it. A directory this scan cannot
// read reports false, leaving the archive reader to describe the input.
func zipDirectoryEntries(file *os.File, size int64) (uint64, bool) {
	const (
		endRecordSize   = 22
		maxCommentSize  = 0xffff
		zip64LocatorMax = 20
		zip64RecordSize = 56
		endRecordSig    = 0x06054b50
		zip64LocatorSig = 0x07064b50
		zip64RecordSig  = 0x06064b50
	)
	tailSize := min(size, int64(endRecordSize+maxCommentSize))
	if tailSize < endRecordSize {
		return 0, false
	}
	tail := make([]byte, tailSize)
	if _, err := file.ReadAt(tail, size-tailSize); err != nil {
		return 0, false
	}
	position := bytes.LastIndex(tail, []byte("PK\x05\x06"))
	if position < 0 || len(tail)-position < endRecordSize {
		return 0, false
	}
	record := tail[position:]
	if binary.LittleEndian.Uint32(record) != endRecordSig {
		return 0, false
	}
	entries := uint64(binary.LittleEndian.Uint16(record[10:12]))
	locatorStart := position - zip64LocatorMax
	if locatorStart < 0 {
		return entries, true
	}
	locator := tail[locatorStart:]
	if binary.LittleEndian.Uint32(locator) != zip64LocatorSig {
		return entries, true
	}
	counted := make([]byte, zip64RecordSize)
	offset := binary.LittleEndian.Uint64(locator[8:16])
	if _, err := file.ReadAt(counted, int64(offset)); err != nil {
		return entries, true
	}
	if binary.LittleEndian.Uint32(counted) != zip64RecordSig {
		return entries, true
	}
	return binary.LittleEndian.Uint64(counted[32:40]), true
}

// scan validates every entry name and removes a single wrapper directory.
func (a *archive) scan(ctx context.Context) error {
	var raw []entry
	err := a.visit(ctx, false, func(ordinal int, v visited, _ io.Reader) error {
		name, err := normalizeEntryName(v.name)
		if err != nil {
			return fmt.Errorf("entry %q: %w", v.name, err)
		}
		if name == "" {
			return nil
		}
		raw = append(raw, entry{
			ordinal: ordinal,
			name:    name,
			dir:     v.dir,
			size:    v.size,
			exec:    v.exec,
		})
		return nil
	})
	if err != nil {
		return err
	}
	strip := wrapperRoot(raw)
	a.entries = nil
	for _, e := range raw {
		e.rel = stripWrapper(e.name, strip)
		if e.rel == "" {
			continue
		}
		a.entries = append(a.entries, e)
	}
	return nil
}

// normalizeEntryName returns the slash-separated form of an archive name, or
// the empty string for the archive root. It rejects absolute, drive-qualified,
// backslash, and parent-traversing names.
func normalizeEntryName(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%w: empty path", ErrUnsafeEntry)
	}
	if strings.ContainsAny(raw, "\\\x00") {
		return "", fmt.Errorf("%w: %q contains a backslash or NUL", ErrUnsafeEntry, raw)
	}
	if len(raw) > maxPathBytes {
		return "", limitError("archive path is longer than %d bytes", maxPathBytes)
	}
	if strings.Count(raw, "/")+1 > maxPathDepth {
		return "", limitError("archive path is deeper than %d components", maxPathDepth)
	}
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("%w: %q is absolute", ErrUnsafeEntry, raw)
	}
	if len(raw) >= 2 && isASCIILetter(raw[0]) && raw[1] == ':' {
		return "", fmt.Errorf("%w: %q is drive-qualified", ErrUnsafeEntry, raw)
	}
	var parts []string
	for _, part := range strings.Split(raw, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("%w: %q escapes the source root", ErrUnsafeEntry, raw)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/"), nil
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// wrapperRoot returns the one top-level directory that holds every entry, or
// the empty string when the source has root files or several top levels.
func wrapperRoot(entries []entry) string {
	top := ""
	nested := false
	for _, e := range entries {
		first, _, hasChild := strings.Cut(e.name, "/")
		if top == "" {
			top = first
		} else if first != top {
			return ""
		}
		if !hasChild && !e.dir {
			return ""
		}
		if hasChild {
			nested = true
		}
	}
	if !nested {
		return ""
	}
	if serverRootDir(top) && !wrapsServerRoot(entries, top) {
		// The lone folder is itself a server root, as a mods or plugins folder
		// of jars is. Stripping it would scatter its files across the root.
		return ""
	}
	return top
}

// wrapsServerRoot reports whether a lone top-level folder holds a server root
// at its own top level. A config folder that holds server.properties wraps one;
// a mods folder of jars does not.
func wrapsServerRoot(entries []entry, wrapper string) bool {
	prefix := wrapper + "/"
	for _, e := range entries {
		rest, ok := strings.CutPrefix(e.name, prefix)
		if !ok {
			continue
		}
		child, _, _ := strings.Cut(rest, "/")
		if serverRootFiles[child] || serverRootDir(child) {
			return true
		}
	}
	return false
}

// serverRootDir reports whether name is a directory that marks a server root.
func serverRootDir(name string) bool {
	return slices.Contains(serverRootDirs, name)
}

func stripWrapper(name, wrapper string) string {
	if wrapper == "" {
		return name
	}
	if name == wrapper {
		return ""
	}
	return strings.TrimPrefix(name, wrapper+"/")
}

// visit calls fn for every entry in container order. Bodies are readable only
// while fn runs, and only when read is true.
func (a *archive) visit(ctx context.Context, read bool, fn visitFunc) error {
	if a.zip != nil {
		return a.visitZip(ctx, read, fn)
	}
	return a.visitTar(ctx, read, fn)
}

func (a *archive) visitZip(ctx context.Context, read bool, fn visitFunc) error {
	for i, f := range a.zip.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if i >= maxEntries {
			return limitError("archive has more than %d entries", maxEntries)
		}
		mode := f.Mode()
		dir := mode.IsDir() || strings.HasSuffix(f.Name, "/")
		if mode.Type()&^fs.ModeDir != 0 {
			return fmt.Errorf(
				"%w: %q has special type %v; links, devices, and FIFOs are rejected",
				ErrUnsafeEntry,
				f.Name,
				mode.Type(),
			)
		}
		v := visited{name: f.Name, dir: dir, exec: mode.Perm()&0o111 != 0}
		if !dir {
			if f.UncompressedSize64 > uint64(maxFileBytes) {
				return limitError(
					"%q is %d bytes, above the %d byte file limit",
					f.Name,
					f.UncompressedSize64,
					maxFileBytes,
				)
			}
			v.size = int64(f.UncompressedSize64)
		}
		if !read || dir {
			if err := fn(i, v, nil); err != nil {
				return err
			}
			continue
		}
		if err := visitZipBody(f, i, v, fn); err != nil {
			return err
		}
	}
	return nil
}

func visitZipBody(f *zip.File, ordinal int, v visited, fn visitFunc) error {
	r, err := f.Open()
	if err != nil {
		return fmt.Errorf("open %q: %w", f.Name, err)
	}
	fnErr := fn(ordinal, v, r)
	return errors.Join(fnErr, r.Close())
}

func (a *archive) visitTar(ctx context.Context, read bool, fn visitFunc) error {
	if _, err := a.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	gz, err := gzip.NewReader(ctxReader{ctx: ctx, r: a.file})
	if err != nil {
		return fmt.Errorf("read gzip stream: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	ordinal := 0
	// declared sums regular-file header sizes. It is checked after each header
	// and before tr.Next drains that entry's body, so the scan drains at most
	// budget bytes of bodies.
	var declared int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}
		var v visited
		switch hdr.Typeflag {
		case tar.TypeXGlobalHeader:
			continue
		case tar.TypeDir:
			v.dir = true
		case tar.TypeReg, '\x00':
			if hdr.Size < 0 || hdr.Size > maxFileBytes {
				return limitError(
					"%q is %d bytes, above the %d byte file limit",
					hdr.Name,
					hdr.Size,
					maxFileBytes,
				)
			}
			declared += hdr.Size
			if declared > a.budget {
				return limitError(
					"%s declares more than %d extracted bytes",
					a.path,
					a.budget,
				)
			}
			v.size = hdr.Size
		default:
			return fmt.Errorf(
				"%w: %q has special type %q; links, devices, and FIFOs are rejected",
				ErrUnsafeEntry,
				hdr.Name,
				hdr.Typeflag,
			)
		}
		if ordinal >= maxEntries {
			return limitError("archive has more than %d entries", maxEntries)
		}
		v.name = hdr.Name
		v.exec = hdr.Mode&0o111 != 0
		var body io.Reader
		if read && !v.dir {
			body = tr
		}
		if err := fn(ordinal, v, body); err != nil {
			return err
		}
		ordinal++
	}
}

// ctxReader fails each read once ctx is done, so decompression stops at the
// next read after cancellation.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// readEntry returns the body of a zip entry no larger than limit.
func (a *archive) readEntry(e entry, limit int64) ([]byte, error) {
	if a.zip == nil {
		return nil, fmt.Errorf("%s: metadata is read only from zip archives", e.rel)
	}
	f := a.zip.File[e.ordinal]
	if f.UncompressedSize64 > uint64(limit) {
		return nil, limitError("%s is %d bytes, above the %d byte metadata limit", e.rel, f.UncompressedSize64, limit)
	}
	r, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", e.rel, err)
	}
	data, readErr := fsutil.CopyBytes(r, limit)
	return data, errors.Join(readErr, r.Close())
}

// extract streams the bodies of the wanted entries to write, in container order.
func (a *archive) extract(ctx context.Context, wanted map[int]*item, write func(*item, io.Reader) error) error {
	found := 0
	err := a.visit(ctx, true, func(ordinal int, _ visited, body io.Reader) error {
		it, ok := wanted[ordinal]
		if !ok {
			return nil
		}
		found++
		return write(it, body)
	})
	if err != nil {
		return err
	}
	if found != len(wanted) {
		return fmt.Errorf("%s changed between scans: found %d of %d files", a.path, found, len(wanted))
	}
	return nil
}

func (a *archive) rootFile(name string) (entry, bool) {
	for _, e := range a.entries {
		if e.rel == name && !e.dir {
			return e, true
		}
	}
	return entry{}, false
}

func (a *archive) hasPrefix(prefix string) bool {
	for _, e := range a.entries {
		if strings.HasPrefix(e.rel, prefix) {
			return true
		}
	}
	return false
}

func (a *archive) hasRootDir(name string) bool {
	for _, e := range a.entries {
		if (e.dir && e.rel == name) || strings.HasPrefix(e.rel, name+"/") {
			return true
		}
	}
	return false
}

func (a *archive) hasServerFiles() bool {
	for _, e := range a.entries {
		if e.dir || strings.Contains(e.rel, "/") {
			continue
		}
		if serverRootFiles[e.rel] || strings.HasSuffix(strings.ToLower(e.rel), ".jar") {
			return true
		}
	}
	for _, dir := range serverRootDirs {
		if a.hasRootDir(dir) {
			return true
		}
	}
	return false
}

func (a *archive) hasWorldFiles() bool {
	if _, ok := a.rootFile("level.dat"); ok {
		return true
	}
	return a.hasRootDir("region") || a.hasRootDir("playerdata")
}

func classify(a *archive) (Format, error) {
	name := filepath.Base(a.path)
	if _, ok := a.rootFile(modrinthIndexName); ok {
		if a.container != containerZip {
			return "", newFormatError(
				kindInvalidModpack,
				"%s contains %s, but Modrinth modpacks must be .mrpack (zip) archives",
				name,
				modrinthIndexName,
			)
		}
		return FormatMrpack, nil
	}
	if err := a.rejectClientSource(); err != nil {
		return "", err
	}
	if a.mrpack {
		return "", newFormatError(kindInvalidModpack, "%s has no %s at its root", name, modrinthIndexName)
	}
	if a.hasServerFiles() {
		return FormatTree, nil
	}
	if a.hasWorldFiles() {
		return "", newFormatError(
			kindWorldOnly,
			"%s holds world data but no server files; include the server jar, server.properties, or the mods, plugins, or config folders",
			name,
		)
	}
	return "", newFormatError(
		kindUnrecognized,
		"%s has no recognizable server files at its root; expected a server jar, server.properties, eula.txt, or a mods, plugins, or config folder",
		name,
	)
}

// rejectClientSource reports client modpacks, launcher instance exports, and
// override folders that lack a Modrinth index.
func (a *archive) rejectClientSource() error {
	name := filepath.Base(a.path)
	if e, ok := a.rootFile("manifest.json"); ok && a.zip != nil {
		data, err := a.readEntry(e, maxMetadataBytes)
		if err != nil {
			return err
		}
		if isCurseForgeManifest(data) {
			return newFormatError(
				kindClientPack,
				"%s is a CurseForge client pack (manifest.json). lucy imports server packs, not client packs. Export a server pack from the launcher, or archive the server folder itself",
				name,
			)
		}
	}
	if a.hasPrefix(mrpackOverridesPrefix) {
		return newFormatError(
			kindClientPack,
			"%s has an overrides/ folder and no %s, which marks a client modpack. Export a server pack from the launcher, or archive the server folder itself",
			name,
			modrinthIndexName,
		)
	}
	for _, marker := range []string{"mmc-pack.json", "instance.cfg"} {
		if _, ok := a.rootFile(marker); ok {
			return newFormatError(
				kindLauncherInstance,
				"%s is a launcher instance export (%s). lucy cannot import instances. Export a server pack from the launcher, or archive the server folder itself",
				name,
				marker,
			)
		}
	}
	if a.hasPrefix(mrpackServerOverridesPrefix) || a.hasPrefix(mrpackClientOverridesPrefix) {
		return newFormatError(
			kindInvalidModpack,
			"%s has modpack override folders but no %s",
			name,
			modrinthIndexName,
		)
	}
	return nil
}

func isCurseForgeManifest(data []byte) bool {
	var manifest struct {
		ManifestType string `json:"manifestType"`
		Overrides    string `json:"overrides"`
		Files        []any  `json:"files"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false
	}
	return manifest.ManifestType == "minecraftModpack" ||
		(manifest.Overrides != "" && len(manifest.Files) > 0)
}

func buildLayout(a *archive) (*layout, error) {
	format, err := classify(a)
	if err != nil {
		return nil, err
	}
	if format == FormatMrpack {
		return buildMrpackLayout(a)
	}
	return buildTreeLayout(a)
}

// buildTreeLayout plans every entry of a server tree as written as-is.
func buildTreeLayout(a *archive) (*layout, error) {
	table := newPathTable()
	for _, e := range a.entries {
		if _, err := table.add(entryItem(e, e.rel, layerBase)); err != nil {
			return nil, err
		}
	}
	lay, err := newLayout(table.items(), nil)
	if err != nil {
		return nil, err
	}
	if lay.plan.ServerFiles == 0 {
		return nil, newFormatError(
			kindUnrecognized,
			"%s contains no server files",
			filepath.Base(a.path),
		)
	}
	lay.plan.Format = FormatTree
	lay.plan.Name = a.stem
	return lay, nil
}

func entryItem(e entry, rel string, layer int) *item {
	it := &item{
		rel:     rel,
		dir:     e.dir,
		mode:    fileMode(e.exec),
		size:    e.size,
		layer:   layer,
		ordinal: e.ordinal,
	}
	if e.dir {
		it.mode = dirMode
		it.size = 0
		it.ordinal = -1
	}
	return it
}

// fileMode keeps only the executable bit of a source file.
func fileMode(exec bool) fs.FileMode {
	if exec {
		return 0o755
	}
	return 0o644
}

// materializer writes a layout beneath dest through an os.Root, so no write can
// follow a link out of the destination.
type materializer struct {
	ctx      context.Context
	dest     string
	root     *os.Root
	stage    string
	network  *cache.Client
	progress Progress
	budget   *byteBudget
}

func newMaterializer(
	ctx context.Context,
	dest string,
	network *cache.Client,
	progress Progress,
) (*materializer, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	if err := prepareDestination(abs); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	return &materializer{
		ctx:      ctx,
		dest:     abs,
		root:     root,
		network:  network,
		progress: progress,
		budget:   &byteBudget{left: maxExtractedBytes},
	}, nil
}

// prepareDestination creates dest when absent and refuses a non-empty target,
// so that existing files are never replaced.
func prepareDestination(dest string) error {
	info, err := os.Stat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		return os.MkdirAll(dest, dirMode)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("destination %s is not a directory", dest)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("destination %s is not empty", dest)
	}
	return nil
}

func (m *materializer) cleanup() {
	if m.stage != "" {
		_ = os.RemoveAll(m.stage)
	}
	_ = m.root.Close()
}

func (m *materializer) run(a *archive, lay *layout) error {
	wanted := make(map[int]*item)
	for _, it := range lay.items {
		if err := m.ctx.Err(); err != nil {
			return err
		}
		if it.dir {
			if err := m.ensureDir(it.rel); err != nil {
				return err
			}
			continue
		}
		if it.download == nil {
			wanted[it.ordinal] = it
		}
	}
	for _, it := range lay.items {
		if it.dir || it.download == nil {
			continue
		}
		if err := m.fetch(it); err != nil {
			return err
		}
	}
	return a.extract(m.ctx, wanted, m.writeFile)
}

func (m *materializer) ensureDir(rel string) error {
	if rel == "" {
		return nil
	}
	current := ""
	for _, part := range strings.Split(rel, "/") {
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		native := filepath.FromSlash(current)
		info, err := m.root.Lstat(native)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%w: %s is not a directory", ErrUnsafeEntry, current)
			}
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := m.root.Mkdir(native, dirMode); err != nil {
			return err
		}
		if err := m.root.Chmod(native, dirMode); err != nil {
			return err
		}
	}
	return nil
}

func (m *materializer) writeFile(it *item, body io.Reader) error {
	if err := m.ensureDir(parentRel(it.rel)); err != nil {
		return err
	}
	file, err := m.root.OpenFile(
		filepath.FromSlash(it.rel),
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create %s: %w", it.rel, err)
	}
	_, copyErr := io.Copy(file, newChargedReader(m.ctx, body, m.budget))
	chmodErr := file.Chmod(it.mode)
	if err := errors.Join(copyErr, chmodErr, file.Close()); err != nil {
		return fmt.Errorf("write %s: %w", it.rel, err)
	}
	return nil
}

// ensureStage creates the private staging directory that holds downloads
// before they are renamed into place. It lives inside dest, so the rename never
// crosses a filesystem.
func (m *materializer) ensureStage() error {
	if m.stage != "" {
		return nil
	}
	stage, err := os.MkdirTemp(m.dest, ".importpack-stage-")
	if err != nil {
		return err
	}
	m.stage = stage
	return nil
}

// byteBudget bounds the bytes written to the destination across all sources.
type byteBudget struct {
	left int64
	used int64
}

func (b *byteBudget) spend(n int64) error {
	if n > b.left {
		return limitError("extraction exceeds the %d byte limit", maxExtractedBytes)
	}
	b.left -= n
	b.used += n
	return nil
}

func (b *byteBudget) refund(n int64) {
	b.left += n
	b.used -= n
}

// chargedReader debits every byte it returns from the budget before the
// caller can write it, and fails on the first byte beyond a file limit.
type chargedReader struct {
	ctx    context.Context
	r      io.Reader
	budget *byteBudget
	read   int64
}

func newChargedReader(ctx context.Context, r io.Reader, budget *byteBudget) *chargedReader {
	return &chargedReader{ctx: ctx, r: r, budget: budget}
}

func (c *chargedReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if n == 0 {
		return 0, err
	}
	n64 := int64(n)
	if c.read+n64 > maxFileBytes {
		return 0, limitError("file exceeds the %d byte file limit", maxFileBytes)
	}
	if spendErr := c.budget.spend(n64); spendErr != nil {
		return 0, spendErr
	}
	c.read += n64
	return n, err
}

// refund returns the bytes of an attempt that failed.
func (c *chargedReader) refund() {
	c.budget.refund(c.read)
	c.read = 0
}

func sniffContainer(path string) (containerKind, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	magic := make([]byte, 4)
	n, err := io.ReadFull(file, magic)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, err
	}
	magic = magic[:n]
	switch {
	case bytes.HasPrefix(magic, []byte("PK\x03\x04")) || bytes.HasPrefix(magic, []byte("PK\x05\x06")):
		return containerZip, nil
	case bytes.HasPrefix(magic, []byte{0x1f, 0x8b}):
		return containerTarGzip, nil
	default:
		return 0, newFormatError(
			kindUnsupportedArchive,
			"%s is not a zip, mrpack, or tar.gz archive",
			filepath.Base(path),
		)
	}
}
