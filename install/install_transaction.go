package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/workspace/discovery"
	"gopkg.in/yaml.v3"
)

type (
	receiptEntry struct {
		Path   string `yaml:"path"`
		SHA256 string `yaml:"sha256"`
		Mode   uint32 `yaml:"mode"`
	}
	installReceipt struct {
		Version         int            `yaml:"version"`
		LockFingerprint string         `yaml:"lock_fingerprint"`
		Entries         []receiptEntry `yaml:"entries"`
	}
)

// RecordAdoptedPackages records package files that initialization identified
// from upstream metadata as owned runtime outputs. Initialization stages
// nothing, so the receipt answers no lock; ownership only lets a later
// installation replace or prune these files instead of refusing them as
// unmanaged files. Packages initialization left unidentified, files outside
// the workspace, and paths that traverse symlinks stay unowned.
//
// A receipt that does not answer the published lock cannot describe the
// installation either, so its fingerprint is cleared as well; a receipt that
// still answers the lock keeps it.
func RecordAdoptedPackages(root string, d *document.Lock, paths []string) error {
	fingerprint, err := ResolutionFingerprint(d)
	if err != nil {
		return err
	}
	receiptPath := filepath.Join(root, ".lucy", "install.yaml")
	receipt := installReceipt{Version: 1, Entries: []receiptEntry{}}
	raw, err := os.ReadFile(receiptPath)
	exists := err == nil
	switch {
	case exists:
		if err := yaml.Unmarshal(raw, &receipt); err != nil {
			return err
		}
	case !os.IsNotExist(err):
		return err
	}
	answersLock := exists && receipt.LockFingerprint == fingerprint
	entries := make(map[string]receiptEntry, len(receipt.Entries)+len(paths))
	for _, entry := range receipt.Entries {
		entries[entry.Path] = entry
	}
	recorded := false
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil {
			continue
		}
		if err := workspacewrite.SafePath(root, relative); err != nil {
			// Lucy does not own files outside the workspace, and installation
			// refuses to prune through symlinks.
			continue
		}
		relative = filepath.ToSlash(relative)
		info, err := os.Stat(absolute)
		if err != nil {
			return err
		}
		hashes, err := DigestFile(absolute, nil)
		if err != nil {
			return err
		}
		entry := receiptEntry{
			Path:   relative,
			SHA256: hashes["sha256"],
			Mode:   uint32(info.Mode().Perm()),
		}
		if entries[relative] == entry {
			continue
		}
		entries[relative] = entry
		recorded = true
	}
	if !recorded && (receipt.LockFingerprint == "" || answersLock) {
		return nil
	}
	// Initialization staged nothing, so the receipt cannot answer the lock it
	// publishes.
	receipt.Version = 1
	receipt.LockFingerprint = ""
	receipt.Entries = make([]receiptEntry, 0, len(entries))
	for _, entry := range entries {
		receipt.Entries = append(receipt.Entries, entry)
	}
	slices.SortFunc(receipt.Entries, func(a, b receiptEntry) int {
		return strings.Compare(a.Path, b.Path)
	})
	receiptBytes, err := yaml.Marshal(receipt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o755); err != nil {
		return err
	}
	return fsutil.AtomicWrite(receiptPath, receiptBytes, 0o600)
}

// receiptAdoptsRuntime reports whether the observed runtime stays adoptable. A
// receipt that answers a lock owns the runtime it installed. Initialization
// writes a receipt without a lock fingerprint, so it records the packages it
// identified while the observed runtime remains the installation to adopt.
func receiptAdoptsRuntime(root string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".lucy", "install.yaml"))
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var receipt installReceipt
	if err := yaml.Unmarshal(raw, &receipt); err != nil {
		return false, err
	}
	return receipt.LockFingerprint == "", nil
}

// adoptedOutputPaths reports the workspace-relative members of the runtime the
// lock names, when observation selects that runtime. Installation replaces
// these files instead of refusing them: they belong to the installation that
// initialization adopted, not to unrelated work.
func adoptedOutputPaths(root string, server document.LockedServer, logger *log.Logger) map[string]bool {
	serverDir := filepath.Join(root, filepath.FromSlash(server.Directory))
	observed := discovery.Discover(serverDir)
	declaration := &discovery.Declaration{
		Product:     server.Product,
		Minecraft:   server.Minecraft,
		CoreVersion: server.CoreVersion,
	}
	if err := observed.Select(discovery.Request{Declared: declaration}); err != nil {
		logger.Debug("no adopted installation to reconcile", "reason", err)
		return nil
	}
	selected := observed.Selected()
	if selected == nil || selected.Installation == nil {
		return nil
	}
	paths := make(map[string]bool, len(selected.Installation.Files))
	for _, file := range selected.Installation.Files {
		relative, err := filepath.Rel(root, file.Path)
		if err != nil || !filepath.IsLocal(relative) {
			continue
		}
		paths[filepath.ToSlash(relative)] = true
	}
	return paths
}

func applyCommit(root, stage string, d *document.Manifest, l *document.Lock, publish bool, overwritePaths, adoptedPaths map[string]bool, logger *log.Logger) (FileChanges, error) {
	var counts FileChanges
	resultErr := applyCommitFiles(root, stage, d, l, publish, overwritePaths, adoptedPaths, &counts, logger)
	return counts, resultErr
}

func applyCommitFiles(root, stage string, d *document.Manifest, l *document.Lock, publish bool, overwritePaths, adoptedPaths map[string]bool, counts *FileChanges, logger *log.Logger) (resultErr error) {
	receiptPath := filepath.Join(root, ".lucy", "install.yaml")
	previous := installReceipt{}
	raw, err := os.ReadFile(receiptPath)
	receiptExisted := err == nil
	if receiptExisted {
		if err := yaml.Unmarshal(raw, &previous); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	owned := map[string]string{}
	occupantHashes := map[string]string{}
	for _, entry := range previous.Entries {
		owned[entry.Path] = entry.SHA256
	}
	next := installReceipt{Version: 1, Entries: []receiptEntry{}}
	lockFingerprint, err := ResolutionFingerprint(l)
	if err != nil {
		return err
	}
	next.LockFingerprint = lockFingerprint
	tracked := trackedPaths(l)
	sources := map[string]string{}
	sourceImages := map[string]workspacewrite.InstallImage{}
	err = filepath.WalkDir(stage, func(p string, e os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if e.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(stage, p)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if strings.Contains(relative, "/.bootstrap-data/") || strings.HasPrefix(relative, ".bootstrap-data/") {
			return nil
		}
		if err := document.SafeRelativePath(relative); err != nil {
			return err
		}
		target := filepath.Join(root, relative)
		mutableDefault := filepath.Base(relative) == "config.yml" || filepath.Base(relative) == "user_jvm_args.txt"
		if tracked[strings.ToLower(relative)] {
			if !mutableDefault {
				return fmt.Errorf("tracked file %s collides with managed runtime output", relative)
			}
			return nil
		}
		if mutableDefault {
			if _, err := os.Stat(target); err == nil {
				return nil
			}
		}
		hashes, err := DigestFile(p, nil)
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if _, err := os.Lstat(target); err == nil {
			if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("refuse symlink output %s", relative)
			}
			actual, err := DigestFile(target, nil)
			if err != nil {
				return err
			}
			occupantHashes[relative] = actual["sha256"]
			if actual["sha256"] != hashes["sha256"] && actual["sha256"] != owned[relative] {
				switch {
				case overwritePaths[relative]:
					logger.Debug(
						"overwriting occupied output",
						"path", relative,
						"reason", "overwrite requested",
					)
				case adoptedPaths[relative]:
					logger.Debug(
						"adopting occupied output",
						"path", relative,
						"reason", "occupied by the adopted installation",
					)
				default:
					return fmt.Errorf("unowned or modified output conflict: %s", relative)
				}
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		mode := os.FileMode(0o644)
		if !mutableDefault {
			mode = info.Mode().Perm()
			next.Entries = append(next.Entries, receiptEntry{
				Path:   relative,
				SHA256: hashes["sha256"],
				Mode:   uint32(mode),
			})
		}
		sourceImages[relative] = workspacewrite.InstallImage{
			SHA256: hashes["sha256"],
			Mode:   effectiveInstallMode(mode),
		}
		sources[relative] = p
		return nil
	})
	if err != nil {
		return err
	}
	pruneExpected := map[string]string{}
	for _, old := range previous.Entries {
		if tracked[strings.ToLower(old.Path)] {
			continue
		}
		if _, ok := sources[old.Path]; ok {
			continue
		}
		p := filepath.Join(root, old.Path)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		actual, err := DigestFile(p, nil)
		if err != nil {
			return err
		}
		if actual["sha256"] != old.SHA256 {
			return fmt.Errorf("modified owned output cannot be pruned: %s", old.Path)
		}
		pruneExpected[old.Path] = old.SHA256
	}
	receiptBytes, err := yaml.Marshal(next)
	if err != nil {
		return err
	}
	lockPlan, err := installDocumentPlanFor(
		root,
		document.LockFilename,
		0o644,
		func() ([]byte, error) { return document.LockFile(root).Snapshot(l) },
		l.MergeBytes,
	)
	if err != nil {
		return err
	}
	var manifestPlan *installDocumentPlan
	if publish {
		manifestPlan, err = installDocumentPlanFor(
			root,
			document.ManifestFilename,
			0o644,
			func() ([]byte, error) { return document.ManifestFile(root).Snapshot(d) },
			d.MergeBytes,
		)
		if err != nil {
			return err
		}
	}
	receiptMode, err := installFileMode(receiptPath, 0o600)
	if err != nil {
		return err
	}
	receiptAfter := installBytesImage(receiptBytes, receiptMode)
	var receiptBase *workspacewrite.InstallImage
	if receiptExisted {
		receiptBase = installBytesImage(raw, receiptMode)
	}
	afterImages := make(map[string]*workspacewrite.InstallImage, len(sources)+3)
	for name, image := range sourceImages {
		if installPathReserved(root, name) {
			return fmt.Errorf("staged output %s is reserved", name)
		}
		image := image
		afterImages[name] = &image
	}
	planAfter := func(name string, image *workspacewrite.InstallImage) error {
		if installPathMatchesReserved(
			root, name, workspacewrite.InstallJournalName,
		) {
			return fmt.Errorf("installation path %s is reserved", name)
		}
		if _, exists := afterImages[name]; exists {
			return fmt.Errorf("installation path %s has conflicting owners", name)
		}
		afterImages[name] = image
		return nil
	}
	if err := planAfter(document.LockFilename, &lockPlan.Image); err != nil {
		return err
	}
	if publish {
		if err := planAfter(document.ManifestFilename, &manifestPlan.Image); err != nil {
			return err
		}
	}
	if err := planAfter(".lucy/install.yaml", receiptAfter); err != nil {
		return err
	}
	baseImages := map[string]*workspacewrite.InstallImage{
		document.LockFilename: lockPlan.Base,
		".lucy/install.yaml":  receiptBase,
	}
	documentAfter := map[string]*workspacewrite.InstallImage{
		document.LockFilename: &lockPlan.Image,
		".lucy/install.yaml":  receiptAfter,
	}
	if publish {
		baseImages[document.ManifestFilename] = manifestPlan.Base
		documentAfter[document.ManifestFilename] = &manifestPlan.Image
	}
	for _, old := range previous.Entries {
		if tracked[strings.ToLower(old.Path)] {
			continue
		}
		if _, ok := sources[old.Path]; ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, old.Path)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if installPathMatchesReserved(
			root, old.Path, document.ManifestFilename,
		) {
			return fmt.Errorf("installation receipt path %s is reserved", old.Path)
		}
		if err := planAfter(old.Path, nil); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(afterImages))
	for name := range afterImages {
		names = append(names, name)
	}
	slices.Sort(names)
	if err := rejectInstallPathAliases(root, names); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "rollback-*")
	if err != nil {
		return err
	}
	journalPublished := false
	defer func() {
		if journalPublished {
			return
		}
		if err := os.RemoveAll(backup); err != nil {
			logger.Warn(fmt.Errorf("remove unused installation backup: %w", err))
		}
	}()
	backupRelative := filepath.ToSlash(
		filepath.Join(".lucy", filepath.Base(backup)),
	)
	journal := workspacewrite.InstallJournal{
		Backup:  backupRelative,
		Entries: make([]workspacewrite.InstallJournalEntry, 0, len(names)),
	}
	prepareErr := func() (resultErr error) {
		files, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
		for _, name := range names {
			if err := workspacewrite.SafePath(root, name); err != nil {
				return err
			}
			target := filepath.Join(root, name)
			entry := workspacewrite.InstallJournalEntry{
				Path:  name,
				After: afterImages[name],
			}
			info, err := files.Lstat(name)
			switch {
			case err == nil:
				if !info.Mode().IsRegular() {
					return fmt.Errorf("installation target %s is not a regular file", name)
				}
				hashes, err := DigestFile(target, nil)
				if err != nil {
					return err
				}
				before := workspacewrite.InstallImage{
					SHA256: hashes["sha256"],
					Mode:   effectiveInstallMode(info.Mode().Perm()),
				}
				entry.Before = &before
			case errors.Is(err, os.ErrNotExist):
			default:
				return err
			}
			if base, planned := baseImages[name]; planned {
				if !workspacewrite.InstallImagesEqual(entry.Before, base) {
					return fmt.Errorf(
						"installation path %s changed while planning",
						name,
					)
				}
			}
			if entry.Before == nil && entry.After == nil {
				continue
			}
			if entry.After == nil {
				expected, validated := pruneExpected[name]
				if !validated || entry.Before == nil ||
					entry.Before.SHA256 != expected {
					return fmt.Errorf(
						"modified or unowned output cannot be pruned: %s",
						name,
					)
				}
			}
			if entry.Before != nil && entry.After != nil &&
				workspacewrite.InstallImagesEqual(entry.Before, entry.After) {
				continue
			}
			if entry.Before != nil {
				if err := preserveInstallPreImage(
					files, backup, backupRelative, *entry.Before, target,
				); err != nil {
					return err
				}
			}
			journal.Entries = append(journal.Entries, entry)
		}
		return nil
	}()
	if prepareErr != nil {
		return prepareErr
	}
	journalBytes, err := yaml.Marshal(journal)
	if err != nil {
		return err
	}
	journalPath := filepath.Join(root, workspacewrite.InstallJournalName)
	if err := fsutil.AtomicWrite(journalPath, journalBytes, 0o600); err != nil {
		// AtomicWrite can return a sync error after the journal replacement was
		// published. Keep the backup while any journal remains, so recovery can
		// still verify and remove it.
		if _, statErr := os.Lstat(journalPath); statErr == nil {
			journalPublished = true
		}
		return err
	}
	journalPublished = true
	logger.Debug(
		"installation transaction journaled",
		"journal", journalPath,
		"backup", journal.Backup,
		"paths", len(journal.Entries),
	)
	mutations := make(map[string]workspacewrite.InstallJournalEntry, len(journal.Entries))
	for _, entry := range journal.Entries {
		mutations[entry.Path] = entry
	}
	failed := true
	defer func() {
		if !failed {
			return
		}
		committed, _ := workspacewrite.InstallJournalCommitted(root)
		resultErr = errors.Join(resultErr, workspacewrite.RecoverInstall(root, logger))
		if committed && resultErr != nil {
			resultErr = &CommittedError{Err: fmt.Errorf(
				"installation committed; %w", resultErr,
			)}
		}
	}()
	applyErr := func() (resultErr error) {
		files, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
		for name, source := range sources {
			entry, mutated := mutations[name]
			if !mutated {
				continue
			}
			image := sourceImages[name]
			_, statErr := files.Lstat(name)
			exists := statErr == nil
			if !exists {
				counts.Created++
			} else if occupantHashes[name] != image.SHA256 {
				counts.Replaced++
			}
			parent := filepath.Dir(name)
			if parent != "." {
				if err := files.MkdirAll(parent, 0o755); err != nil {
					return err
				}
			}
			input, err := os.Open(source)
			if err != nil {
				return err
			}
			copyErr := fsutil.WriteRoot(
				files,
				name,
				input,
				fsutil.WriteOptions{
					Mode:    os.FileMode(entry.After.Mode),
					Replace: true,
					Check:   installBeforeCheck(files, entry),
				},
			)
			closeErr := input.Close()
			if copyErr != nil || closeErr != nil {
				return errors.Join(copyErr, closeErr)
			}
		}
		pruned := 0
		for _, old := range previous.Entries {
			if tracked[strings.ToLower(old.Path)] {
				continue
			}
			if _, ok := sources[old.Path]; ok {
				continue
			}
			entry, mutated := mutations[old.Path]
			if !mutated {
				continue
			}
			current, err := observeInstallPath(files, old.Path)
			if err != nil {
				return err
			}
			if !workspacewrite.InstallImagesEqual(current, entry.Before) {
				return fmt.Errorf(
					"installation output %s changed before pruning",
					old.Path,
				)
			}
			if err := files.Remove(old.Path); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return err
			}
			pruned++
		}
		counts.Removed = pruned
		if publish {
			if entry, mutated := mutations[document.ManifestFilename]; mutated {
				if err := publishInstallBytes(
					files,
					document.ManifestFilename,
					manifestPlan.Data,
					&manifestPlan.Image,
					entry,
				); err != nil {
					return err
				}
			}
		}
		if entry, mutated := mutations[document.LockFilename]; mutated {
			if err := publishInstallBytes(
				files,
				document.LockFilename,
				lockPlan.Data,
				&lockPlan.Image,
				entry,
			); err != nil {
				return err
			}
		}
		if entry, mutated := mutations[".lucy/install.yaml"]; mutated {
			if err := publishInstallBytes(
				files,
				".lucy/install.yaml",
				receiptBytes,
				receiptAfter,
				entry,
			); err != nil {
				return err
			}
		}
		for name, expected := range documentAfter {
			current, err := observeInstallPath(files, name)
			if err != nil {
				return err
			}
			compareMode := baseImages[name] != nil
			if !installFinalImageMatches(current, expected, compareMode) {
				return fmt.Errorf(
					"installation document %s changed before commit",
					name,
				)
			}
		}
		for name, expected := range afterImages {
			if _, mutated := mutations[name]; mutated {
				continue
			}
			if _, document := documentAfter[name]; document {
				continue
			}
			current, err := observeInstallPath(files, name)
			if err != nil {
				return err
			}
			if !workspacewrite.InstallImagesEqual(current, expected) {
				return fmt.Errorf(
					"installation path %s changed before commit",
					name,
				)
			}
		}
		return nil
	}()
	if applyErr != nil {
		return applyErr
	}
	journal.Committed = true
	committedBytes, err := yaml.Marshal(journal)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(journalPath, committedBytes, 0o600); err != nil {
		return err
	}
	failed = false
	if err := os.RemoveAll(backup); err != nil {
		return &CommittedError{Err: fmt.Errorf(
			"installation committed; remove rollback backup: %w", err,
		)}
	}
	if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return &CommittedError{Err: fmt.Errorf(
			"installation committed; remove transaction journal: %w", err,
		)}
	}
	logger.Info(
		"installation committed",
		"root", root,
		"files", len(sources),
		"pruned", counts.Removed,
		"manifest-published", publish,
	)
	return nil
}

// installDocumentPlan binds the exact bytes predicted for a document
// publication to its journaled after image. Publication writes Data verbatim,
// so a concurrent comment edit cannot make published bytes differ from the
// journal.
type installDocumentPlan struct {
	Data  []byte
	Base  *workspacewrite.InstallImage
	Image workspacewrite.InstallImage
}

// installDocumentPlanFor predicts the bytes and mode a document write
// publishes. It mirrors document.Service.Write without re-reading the document
// at publication time.
func installDocumentPlanFor(
	root string,
	name string,
	defaultMode os.FileMode,
	snapshot func() ([]byte, error),
	merge func([]byte) ([]byte, error),
) (*installDocumentPlan, error) {
	path := filepath.Join(root, name)
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("write destination %s must be a regular file", name)
		}
		data, err := merge(existing)
		if err != nil {
			return nil, err
		}
		return &installDocumentPlan{
			Data:  data,
			Base:  installBytesImage(existing, info.Mode().Perm()),
			Image: *installBytesImage(data, info.Mode().Perm()),
		}, nil
	case errors.Is(err, os.ErrNotExist):
		data, err := snapshot()
		if err != nil {
			return nil, err
		}
		return &installDocumentPlan{
			Data:  data,
			Image: *installBytesImage(data, defaultMode),
		}, nil
	default:
		return nil, err
	}
}

func installFileMode(path string, defaultMode os.FileMode) (os.FileMode, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("write destination %s must be a regular file", path)
		}
		return info.Mode().Perm(), nil
	case errors.Is(err, os.ErrNotExist):
		return defaultMode, nil
	default:
		return 0, err
	}
}

func installBytesImage(data []byte, mode os.FileMode) *workspacewrite.InstallImage {
	sum := sha256.Sum256(data)
	return &workspacewrite.InstallImage{
		SHA256: hex.EncodeToString(sum[:]),
		Mode:   effectiveInstallMode(mode),
	}
}

// effectiveInstallMode reports the permission bits the platform exposes after
// a chmod to mode. Windows represents regular files as writable (0666) or
// read-only (0444); Unix preserves the requested bits.
func effectiveInstallMode(mode os.FileMode) uint32 {
	if runtime.GOOS == "windows" {
		if mode&0o200 != 0 {
			return 0o666
		}
		return 0o444
	}
	return uint32(mode.Perm())
}

func publishInstallBytes(
	files *os.Root,
	name string,
	data []byte,
	image *workspacewrite.InstallImage,
	entry workspacewrite.InstallJournalEntry,
) error {
	return fsutil.WriteRoot(
		files,
		name,
		bytes.NewReader(data),
		fsutil.WriteOptions{
			Mode:    os.FileMode(image.Mode),
			Replace: true,
			Check:   installBeforeCheck(files, entry),
		},
	)
}

// installBeforeCheck guards a publication against an edit that happened after
// the journal captured the before image.
func installBeforeCheck(
	files *os.Root,
	entry workspacewrite.InstallJournalEntry,
) func() error {
	return func() error {
		current, err := observeInstallPath(files, entry.Path)
		if err != nil {
			return err
		}
		if !workspacewrite.InstallImagesEqual(current, entry.Before) {
			return fmt.Errorf(
				"installation output %s changed before publication",
				entry.Path,
			)
		}
		return nil
	}
}

func observeInstallPath(
	files *os.Root,
	relative string,
) (*workspacewrite.InstallImage, error) {
	info, err := files.Lstat(relative)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf(
			"installation output %s is not a regular file",
			relative,
		)
	}
	file, err := files.Open(relative)
	if err != nil {
		return nil, err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return nil, errors.Join(copyErr, closeErr)
	}
	return &workspacewrite.InstallImage{
		SHA256: hex.EncodeToString(hasher.Sum(nil)),
		Mode:   effectiveInstallMode(info.Mode().Perm()),
	}, nil
}

func installFinalImageMatches(
	current, expected *workspacewrite.InstallImage,
	compareMode bool,
) bool {
	if current == nil || expected == nil {
		return current == nil && expected == nil
	}
	if current.SHA256 != expected.SHA256 {
		return false
	}
	return !compareMode || current.Mode == expected.Mode
}

func installPathReserved(root, name string) bool {
	for _, reserved := range []string{
		workspacewrite.InstallJournalName,
		document.LockFilename,
		document.ManifestFilename,
	} {
		if installPathMatchesReserved(root, name, reserved) {
			return true
		}
	}
	return false
}

// installPathMatchesReserved rejects an exact reserved path always, and a case
// variant only when the target filesystem treats the two names as one entry.
func installPathMatchesReserved(root, name, reserved string) bool {
	if name == reserved {
		return true
	}
	if !strings.EqualFold(name, reserved) {
		return false
	}
	return workspacewrite.InstallPathsAlias(root, reserved, name)
}

// rejectInstallPathAliases refuses names that the target filesystem resolves
// to the same entry. Otherwise one journal entry could overwrite and another
// could prune the same file. Distinct case-sensitive paths stay valid.
func rejectInstallPathAliases(root string, names []string) error {
	aliases := make(map[string][]string, len(names))
	for _, name := range names {
		if !filepath.IsLocal(filepath.FromSlash(name)) ||
			filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name {
			return fmt.Errorf("installation path %q is not canonical", name)
		}
		key := strings.ToLower(name)
		for _, prior := range aliases[key] {
			if workspacewrite.InstallPathsAlias(root, prior, name) {
				return fmt.Errorf(
					"installation paths %q and %q alias the same filesystem entry",
					prior, name,
				)
			}
		}
		aliases[key] = append(aliases[key], name)
	}
	return nil
}

// preserveInstallPreImage stores one complete content-addressed before image
// through a synced root-confined publish and verifies the copy. A failure here
// happens before the journal publishes, so the caller removes the unused
// backup directory.
func preserveInstallPreImage(
	files *os.Root,
	backupPath string,
	backupRelative string,
	image workspacewrite.InstallImage,
	source string,
) error {
	relative := filepath.Join(filepath.FromSlash(backupRelative), image.SHA256)
	if info, err := files.Lstat(relative); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("installation pre-image %s is not a regular file", image.SHA256)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		input, err := os.Open(source)
		if err != nil {
			return err
		}
		copyErr := fsutil.WriteRoot(
			files,
			relative,
			input,
			fsutil.WriteOptions{Mode: 0o600, Replace: true},
		)
		closeErr := input.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
	} else {
		return err
	}
	hashes, err := DigestFile(filepath.Join(backupPath, image.SHA256), nil)
	if err != nil {
		return err
	}
	if hashes["sha256"] != image.SHA256 {
		return fmt.Errorf("installation pre-image %s is incomplete", image.SHA256)
	}
	return nil
}

// ResolutionFingerprint digests the fields an installation receipt pins a lock
// with: the server, MCDR and package fields only. Tracked files, the manifest
// fingerprint, and Java requirement stay out. Changing these fields does not
// change installed files or invalidate the receipt.
func ResolutionFingerprint(l *document.Lock) (string, error) {
	server := l.Server
	server.Java = document.LockedJava{}
	data, err := yaml.Marshal(struct {
		Server   document.LockedServer `yaml:"server"`
		MCDR     *document.LockedMCDR  `yaml:"mcdr,omitempty"`
		Packages []document.Package    `yaml:"packages"`
	}{server, l.MCDR, l.Packages})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func installationCurrent(root string, l *document.Lock, logger *log.Logger) (bool, error) {
	receiptPath := filepath.Join(root, ".lucy", "install.yaml")
	raw, err := os.ReadFile(receiptPath)
	if os.IsNotExist(err) {
		logger.Debug("installation not current", "reason", "no install receipt")
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var receipt installReceipt
	if err := yaml.Unmarshal(raw, &receipt); err != nil {
		return false, err
	}
	expected, err := ResolutionFingerprint(l)
	if err != nil {
		return false, err
	}
	if receipt.LockFingerprint != expected {
		logger.Debug(
			"installation not current",
			"reason", "lock fingerprint changed",
			"receipt", receipt.LockFingerprint,
			"expected", expected,
		)
		return false, nil
	}
	tracked := trackedPaths(l)
	for _, entry := range receipt.Entries {
		if tracked[strings.ToLower(entry.Path)] {
			continue
		}
		if err := workspacewrite.SafePath(root, entry.Path); err != nil {
			return false, err
		}
		actual, err := DigestFile(filepath.Join(root, entry.Path), nil)
		if os.IsNotExist(err) {
			logger.Debug("installation not current", "reason", "owned output missing", "path", entry.Path)
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if actual["sha256"] != entry.SHA256 {
			logger.Debug("installation not current", "reason", "owned output modified", "path", entry.Path)
			return false, nil
		}
	}
	logger.Debug("installation current", "reason", "receipt matches lock and every owned output", "receipt", receiptPath)
	return true, nil
}

func trackedPaths(l *document.Lock) map[string]bool {
	paths := make(map[string]bool, len(l.Files))
	for _, file := range l.Files {
		paths[strings.ToLower(file.Path)] = true
	}
	return paths
}
