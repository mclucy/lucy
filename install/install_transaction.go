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
	"github.com/mclucy/lucy/lockfile"
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
	commitJournal struct {
		Backup  string   `yaml:"backup"`
		Changed []string `yaml:"changed"`
		Created []string `yaml:"created"`
	}
)

func recoverCommit(root string) error {
	p := filepath.Join(root, ".lucy", "commit.yaml")
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal commitJournal
	if err := yaml.Unmarshal(raw, &journal); err != nil {
		return err
	}
	relativeBackup, err := filepath.Rel(filepath.Join(root, ".lucy"), journal.Backup)
	if err != nil || !strings.HasPrefix(relativeBackup, "rollback-") || strings.ContainsAny(relativeBackup, "/\\") {
		return fmt.Errorf("invalid transaction backup directory")
	}
	if err := safeOutputPath(root, filepath.Join(".lucy", relativeBackup)); err != nil {
		return err
	}
	for _, name := range journal.Created {
		if err := safeOutputPath(root, name); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(root, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, name := range journal.Changed {
		if err := safeOutputPath(root, name); err != nil {
			return err
		}
		source := filepath.Join(journal.Backup, name)
		if _, err := os.Stat(source); err == nil {
			info, err := os.Stat(source)
			if err != nil {
				return err
			}
			if err := bootstrap.CopyLockedFile(source, filepath.Join(root, name), info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	return os.RemoveAll(journal.Backup)
}

func applyCommit(root, stage string, d *manifest.Document, l *lockfile.Document, publish bool) (resultErr error) {
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
	for _, entry := range previous.Entries {
		owned[entry.Path] = entry.SHA256
	}
	next := installReceipt{Version: 1, Entries: []receiptEntry{}}
	lockFingerprint, err := resolutionFingerprint(l)
	if err != nil {
		return err
	}
	next.LockFingerprint = lockFingerprint
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
		if err := manifest.SafePath(relative); err != nil {
			return err
		}
		target := filepath.Join(root, relative)
		mutableDefault := filepath.Base(relative) == "config.yml" || filepath.Base(relative) == "user_jvm_args.txt"
		if mutableDefault {
			if _, err := os.Stat(target); err == nil {
				return nil
			}
		}
		hashes, err := digestFile(p, nil)
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
			actual, err := digestFile(target, nil)
			if err != nil {
				return err
			}
			if actual["sha256"] != hashes["sha256"] && actual["sha256"] != owned[relative] {
				return fmt.Errorf("unowned or modified output conflict: %s", relative)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if !mutableDefault {
			next.Entries = append(next.Entries, receiptEntry{Path: relative, SHA256: hashes["sha256"], Mode: uint32(info.Mode().Perm())})
		}
		sources[relative] = p
		return nil
	})
	if err != nil {
		return err
	}
	for _, old := range previous.Entries {
		if _, ok := sources[old.Path]; ok {
			continue
		}
		p := filepath.Join(root, old.Path)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			continue
		}
		actual, err := digestFile(p, nil)
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
	journal := commitJournal{Backup: backup, Changed: []string{}, Created: []string{}}
	names := []string{}
	for name := range sources {
		names = append(names, name)
	}
	for _, entry := range previous.Entries {
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
		if err := safeOutputPath(root, name); err != nil {
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
	if err := manifest.AtomicWrite(filepath.Join(root, ".lucy", "commit.yaml"), journalBytes); err != nil {
		return err
	}
	failed := true
	defer func() {
		if failed {
			resultErr = errors.Join(resultErr, recoverCommit(root))
		}
	}()
	for name, source := range sources {
		mode := os.FileMode(0o644)
		for _, entry := range next.Entries {
			if entry.Path == name {
				mode = os.FileMode(entry.Mode)
				break
			}
		}
		if err := bootstrap.CopyLockedFile(source, filepath.Join(root, name), mode); err != nil {
			return err
		}
	}
	for _, entry := range previous.Entries {
		if _, ok := sources[entry.Path]; !ok {
			if err := os.Remove(filepath.Join(root, entry.Path)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
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
	if err := manifest.AtomicWrite(receiptPath, receiptBytes); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, ".lucy", "commit.yaml")); err != nil {
		return err
	}
	failed = false
	return os.RemoveAll(backup)
}

func safeOutputPath(root, relative string) error {
	if err := manifest.SafePath(relative); err != nil {
		return err
	}
	current := root
	for component := range strings.SplitSeq(filepath.Clean(relative), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path traverses symlink: %s", relative)
		}
	}
	return nil
}

func resolutionFingerprint(l *lockfile.Document) (string, error) {
	data, err := yaml.Marshal(l)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func installationCurrent(root string, l *lockfile.Document) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".lucy", "install.yaml"))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var receipt installReceipt
	if err := yaml.Unmarshal(raw, &receipt); err != nil {
		return false, err
	}
	expected, err := resolutionFingerprint(l)
	if err != nil {
		return false, err
	}
	if receipt.LockFingerprint != expected {
		return false, nil
	}
	for _, entry := range receipt.Entries {
		if err := safeOutputPath(root, entry.Path); err != nil {
			return false, err
		}
		actual, err := digestFile(filepath.Join(root, entry.Path), nil)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if actual["sha256"] != entry.SHA256 {
			return false, nil
		}
	}
	return true, nil
}
