package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
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
// unmanaged files. Packages initialization left unidentified stay unowned.
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
		if err != nil || !filepath.IsLocal(relative) {
			return fmt.Errorf("adopted package %s is outside the workspace", path)
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
	if err == nil {
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
		if !mutableDefault {
			next.Entries = append(next.Entries, receiptEntry{
				Path:   relative,
				SHA256: hashes["sha256"],
				Mode:   uint32(info.Mode().Perm()),
			})
		}
		sources[relative] = p
		return nil
	})
	if err != nil {
		return err
	}
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
	}
	backup, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "rollback-*")
	if err != nil {
		return err
	}
	journal := workspacewrite.InstallJournal{Backup: backup, Changed: []string{}, Created: []string{}}
	names := []string{}
	for name := range sources {
		names = append(names, name)
	}
	for _, entry := range previous.Entries {
		if tracked[strings.ToLower(entry.Path)] {
			continue
		}
		if _, ok := sources[entry.Path]; !ok {
			names = append(names, entry.Path)
		}
	}
	names = append(names, document.LockFilename, ".lucy/install.yaml")
	if publish {
		names = append(names, document.ManifestFilename)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		if err := workspacewrite.SafePath(root, name); err != nil {
			return err
		}
		target := filepath.Join(root, name)
		if _, err := os.Stat(target); err == nil {
			info, err := os.Stat(target)
			if err != nil {
				return err
			}
			if err := bootstrap.CopyLockedFile(target, filepath.Join(backup, name), info.Mode().Perm()); err != nil {
				return err
			}
			journal.Changed = append(journal.Changed, name)
		} else if os.IsNotExist(err) {
			journal.Created = append(journal.Created, name)
		} else {
			return err
		}
	}
	journalBytes, err := yaml.Marshal(journal)
	if err != nil {
		return err
	}
	journalPath := filepath.Join(root, workspacewrite.InstallJournalName)
	if err := fsutil.AtomicWrite(journalPath, journalBytes, 0o600); err != nil {
		return err
	}
	logger.Debug(
		"installation transaction journaled",
		"journal", journalPath,
		"backup", backup,
		"created", len(journal.Created),
		"changed", len(journal.Changed),
	)
	failed := true
	defer func() {
		if failed {
			resultErr = errors.Join(resultErr, workspacewrite.RecoverInstall(root, logger))
		}
	}()
	for name, source := range sources {
		mode := os.FileMode(0o644)
		hash := ""
		for _, entry := range next.Entries {
			if entry.Path == name {
				mode = os.FileMode(entry.Mode)
				hash = entry.SHA256
				break
			}
		}
		target := filepath.Join(root, name)
		_, statErr := os.Lstat(target)
		exists := statErr == nil
		if !exists {
			counts.Created++
		} else if occupantHashes[name] != hash {
			counts.Replaced++
		}
		if err := bootstrap.CopyLockedFile(source, target, mode); err != nil {
			return err
		}
	}
	pruned := 0
	for _, entry := range previous.Entries {
		if tracked[strings.ToLower(entry.Path)] {
			continue
		}
		if _, ok := sources[entry.Path]; !ok {
			if err := os.Remove(filepath.Join(root, entry.Path)); err != nil {
				if !os.IsNotExist(err) {
					return err
				}
				continue
			}
			pruned++
		}
		counts.Removed = pruned
	}
	if publish {
		if err := document.ManifestFile(root).Write(d); err != nil {
			return err
		}
	}
	if err := document.LockFile(root).Write(l); err != nil {
		return err
	}
	receiptBytes, err := yaml.Marshal(next)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(receiptPath, receiptBytes, 0o600); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, workspacewrite.InstallJournalName)); err != nil {
		return err
	}
	failed = false
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	logger.Info(
		"installation committed",
		"root", root,
		"files", len(sources),
		"pruned", pruned,
		"manifest-published", publish,
	)
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
