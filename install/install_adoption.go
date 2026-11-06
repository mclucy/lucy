package install

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/workspace/discovery"
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
	fingerprint, err := receipt.ResolutionFingerprint(d)
	if err != nil {
		return err
	}
	current, err := receipt.Read(root)
	exists := err == nil
	switch {
	case exists:
	case errors.Is(err, os.ErrNotExist):
		empty := receipt.New()
		current = &empty
	default:
		return err
	}
	answersLock := exists && current.LockFingerprint == fingerprint
	entries := make(map[string]receipt.Entry, len(current.Entries)+len(paths))
	for _, entry := range current.Entries {
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
		entry := receipt.Entry{
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
	if !recorded && (current.LockFingerprint == "" || answersLock) {
		return nil
	}
	// Initialization staged nothing, so the receipt cannot answer the lock it
	// publishes.
	next := receipt.New()
	next.Entries = make([]receipt.Entry, 0, len(entries))
	for _, entry := range entries {
		next.Entries = append(next.Entries, entry)
	}
	slices.SortFunc(next.Entries, func(a, b receipt.Entry) int {
		return strings.Compare(a.Path, b.Path)
	})
	return next.Write(root)
}

// receiptAdoptsRuntime reports whether the observed runtime stays adoptable. A
// receipt that answers a lock owns the runtime it installed. Initialization
// writes a receipt without a lock fingerprint, so it records the packages it
// identified while the observed runtime remains the installation to adopt.
func receiptAdoptsRuntime(root string) (bool, error) {
	current, err := receipt.Read(root)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return current.LockFingerprint == "", nil
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
