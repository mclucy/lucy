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
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
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

func applyCommit(root, stage string, d *manifest.Document, l *lockfile.Document, publish bool, overwritePaths map[string]bool) (FileChanges, error) {
	var counts FileChanges
	resultErr := applyCommitFiles(root, stage, d, l, publish, overwritePaths, &counts)
	return counts, resultErr
}

func applyCommitFiles(root, stage string, d *manifest.Document, l *lockfile.Document, publish bool, overwritePaths map[string]bool, counts *FileChanges) (resultErr error) {
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
		if err := manifest.SafeRelativePath(relative); err != nil {
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
				if !overwritePaths[relative] {
					return fmt.Errorf("unowned or modified output conflict: %s", relative)
				}
				log.Debug(
					"overwriting occupied output",
					"path", relative,
					"reason", "overwrite requested",
				)
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
	names = append(names, lockfile.Filename, ".lucy/install.yaml")
	if publish {
		names = append(names, manifest.Filename)
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
	log.Debug(
		"installation transaction journaled",
		"journal", journalPath,
		"backup", backup,
		"created", len(journal.Created),
		"changed", len(journal.Changed),
	)
	failed := true
	defer func() {
		if failed {
			resultErr = errors.Join(resultErr, workspacewrite.RecoverInstall(root))
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
		if err := manifest.Write(root, d); err != nil {
			return err
		}
	}
	if err := lockfile.Write(root, l); err != nil {
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
	log.Info(
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
// fingerprint, and the Java requirement stay out, so changes to them keep a
// receipt current: they change no installed file.
func ResolutionFingerprint(l *lockfile.Document) (string, error) {
	server := l.Server
	server.Java = lockfile.Java{}
	data, err := yaml.Marshal(struct {
		Server   lockfile.Server    `yaml:"server"`
		MCDR     *lockfile.MCDR     `yaml:"mcdr,omitempty"`
		Packages []lockfile.Package `yaml:"packages"`
	}{server, l.MCDR, l.Packages})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// legacyResolutionFingerprint reproduces the whole-document fingerprint that
// receipts recorded before the resolved runtime was separated from the lock
// identity, so such a receipt stays current instead of re-staging once.
func legacyResolutionFingerprint(l *lockfile.Document) (string, error) {
	data, err := yaml.Marshal(struct {
		Version             int                `yaml:"lock_version"`
		ManifestFingerprint string             `yaml:"manifest_fingerprint"`
		Server              lockfile.Server    `yaml:"server"`
		MCDR                *lockfile.MCDR     `yaml:"mcdr,omitempty"`
		Packages            []lockfile.Package `yaml:"packages"`
	}{l.SchemaVersion, l.ManifestFingerprint, l.Server, l.MCDR, l.Packages})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func installationCurrent(root string, l *lockfile.Document) (bool, error) {
	receiptPath := filepath.Join(root, ".lucy", "install.yaml")
	raw, err := os.ReadFile(receiptPath)
	if os.IsNotExist(err) {
		log.Debug("installation not current", "reason", "no install receipt")
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
		legacy, err := legacyResolutionFingerprint(l)
		if err != nil {
			return false, err
		}
		if receipt.LockFingerprint != legacy {
			log.Debug(
				"installation not current",
				"reason", "lock fingerprint changed",
				"receipt", receipt.LockFingerprint,
				"expected", expected,
			)
			return false, nil
		}
		log.Debug("accepting a pre-file-tracking installation receipt", "receipt", receiptPath)
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
			log.Debug("installation not current", "reason", "owned output missing", "path", entry.Path)
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if actual["sha256"] != entry.SHA256 {
			log.Debug("installation not current", "reason", "owned output modified", "path", entry.Path)
			return false, nil
		}
	}
	log.Debug("installation current", "reason", "receipt matches lock and every owned output", "receipt", receiptPath)
	return true, nil
}

func trackedPaths(l *lockfile.Document) map[string]bool {
	paths := make(map[string]bool, len(l.Files))
	for _, file := range l.Files {
		paths[strings.ToLower(file.Path)] = true
	}
	return paths
}
