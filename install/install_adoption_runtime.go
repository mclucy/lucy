package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/bootstrap/snapshot"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
)

func adoptRuntime(
	files *os.Root,
	root string,
	opts AdoptOptions,
	tracked map[string]bool,
	ledger *adoptionLedger,
	check currentCheck,
) error {
	if opts.Lock.Server.Bootstrap == "" && opts.Lock.Server.Product == "" {
		return nil
	}
	baselines := runtimeBaselines(root, opts.RuntimeFiles)
	if opts.Image == nil {
		return adoptPinnedRuntime(files, root, opts, tracked, baselines, ledger)
	}

	entries := make([]snapshot.Entry, 0, len(opts.Image))
	var skipped int
	for _, out := range opts.Image {
		if out.Role != bootstrap.RoleRuntime || out.Policy == bootstrap.PolicyDiscard {
			continue
		}
		if out.Policy == bootstrap.PolicyManaged && runtimePathReserved(root, out.Path, tracked) {
			skipped++
			continue
		}
		entries = append(entries, snapshot.Entry{
			Path:   out.Path,
			SHA256: out.SHA256,
			Mode:   out.Mode,
			Policy: string(out.Policy),
		})
	}
	compared, err := snapshot.CompareCached(root, entries, check.memo)
	if err != nil {
		return fmt.Errorf("compare existing runtime with the locked image: %w", err)
	}

	claim := func(entry snapshot.Entry) (bool, error) {
		expected := &workspacewrite.InstallImage{
			SHA256: entry.SHA256,
			Mode:   workspacewrite.EffectiveInstallMode(os.FileMode(entry.Mode)),
		}
		if !runtimeBaselineMatches(entry.Path, expected, baselines) {
			return false, nil
		}
		return true, ledger.claim(entry.Path, receipt.OriginBootstrap, "", expected)
	}
	for _, entry := range compared.Matching {
		if entry.Policy != string(bootstrap.PolicyManaged) {
			continue
		}
		claimed, err := claim(entry)
		if err != nil {
			return err
		}
		if !claimed {
			skipped++
		}
	}
	// A file with the image's bytes but other permissions stays owned with
	// its observed mode. The fingerprint is withheld, so the next install
	// restores the image's permissions.
	var otherPermissions []string
	for _, entry := range compared.OtherPermissions {
		claimed, err := claim(entry)
		if err != nil {
			return err
		}
		if !claimed {
			skipped++
			continue
		}
		otherPermissions = append(otherPermissions, entry.Path)
	}
	if len(otherPermissions) > 0 {
		ledger.warnings = append(ledger.warnings, fmt.Errorf(
			"runtime files have other permissions than the runtime image: %s; the next lucy install restores them",
			strings.Join(otherPermissions, ", "),
		))
	}

	runtimeDiffers := len(otherPermissions) > 0
	for _, entry := range compared.Differing {
		if entry.Policy == string(bootstrap.PolicyManaged) {
			runtimeDiffers = true
			continue
		}
		// An edited seed still counts as present; another kind of file at
		// a seed path does not.
		if !regularFileWithin(files, entry.Path) {
			runtimeDiffers = true
		}
	}
	if opts.Logger != nil {
		opts.Logger.Debug(
			"compared existing server with the locked runtime image",
			"matching", len(compared.Matching),
			"differing", len(compared.Differing),
			"other-permissions", len(compared.OtherPermissions),
			"missing", len(compared.Missing),
			"skipped", skipped,
		)
	}
	// Missing seeds require a stage, but existing seeds stay user-owned.
	if runtimeDiffers || len(compared.Missing) > 0 || skipped > 0 {
		return nil
	}
	fingerprint, err := receipt.BootstrapFingerprint(opts.Lock)
	if err != nil {
		return err
	}
	ledger.bootstrapFingerprint = fingerprint
	ledger.seedPaths = observedSeedPaths(root, opts.Image, tracked)
	return nil
}

func regularFileWithin(files *os.Root, rel string) bool {
	info, err := files.Lstat(filepath.FromSlash(rel))
	return err == nil && info.Mode().IsRegular()
}

func runtimeBaselines(root string, observed []AdoptedRuntimeFile) map[string]AdoptedRuntimeFile {
	baselines := make(map[string]AdoptedRuntimeFile, len(observed))
	for _, file := range observed {
		if rel, ok := workspaceRelative(root, file.Path); ok {
			baselines[rel] = file
		}
	}
	return baselines
}

func runtimeBaselineMatches(
	rel string,
	current *workspacewrite.InstallImage,
	baselines map[string]AdoptedRuntimeFile,
) bool {
	observed, ok := baselines[rel]
	return !ok || (current.Mode == observed.ExpectedMode &&
		strings.EqualFold(current.SHA256, observed.ExpectedSHA256))
}

func adoptPinnedRuntime(
	files *os.Root,
	root string,
	opts AdoptOptions,
	tracked map[string]bool,
	baselines map[string]AdoptedRuntimeFile,
	ledger *adoptionLedger,
) error {
	server := opts.Lock.Server
	if err := document.SafeRelativePath(server.Directory); err != nil {
		return err
	}
	// Only managed runtime outputs enter the receipt. A pinned input that the
	// method discards after staging, such as the Forge mappings, stays with
	// the user, as it does after a fresh install.
	claim := func(serverRel string, pins map[string]string) error {
		spec, declared, err := bootstrap.ClassifyOutput(server, serverRel)
		if err != nil {
			return err
		}
		if !declared || spec.Policy != bootstrap.PolicyManaged || spec.Role != bootstrap.RoleRuntime {
			return nil
		}
		rel := filepath.ToSlash(filepath.Join(server.Directory, filepath.FromSlash(serverRel)))
		if runtimePathReserved(root, rel, tracked) || !workspacePathSafe(root, rel) {
			return nil
		}
		if err := requirePinnedArtifact(pins, rel); err != nil {
			return nil
		}
		current, err := pinnedRuntimeImage(files, rel, pins)
		if err != nil || current == nil {
			return err
		}
		if !runtimeBaselineMatches(rel, current, baselines) {
			return nil
		}
		return ledger.claim(rel, receipt.OriginBootstrap, "", current)
	}
	for _, input := range server.Inputs {
		rel, err := bootstrap.InputPath(server.Bootstrap, input)
		if err != nil {
			return err
		}
		if err := claim(rel, input.Hashes); err != nil {
			return err
		}
	}
	corePath, err := bootstrap.CorePath(server)
	if err != nil {
		return err
	}
	if corePath != "" {
		return claim(corePath, server.Artifact.Hashes)
	}
	return nil
}

func pinnedRuntimeImage(
	files *os.Root,
	rel string,
	pins map[string]string,
) (*workspacewrite.InstallImage, error) {
	info, err := files.Lstat(rel)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("inspect pinned runtime file %s: %w", rel, err)
	case !info.Mode().IsRegular():
		return nil, nil
	}
	file, err := files.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("open pinned runtime file %s: %w", rel, err)
	}
	hashes, err := digestReader(file, rel, pins)
	if closeErr := file.Close(); closeErr != nil {
		return nil, errors.Join(err, closeErr)
	}
	if errors.Is(err, errArtifactDigestMismatch) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("digest pinned runtime file %s: %w", rel, err)
	}
	return &workspacewrite.InstallImage{
		SHA256: hashes["sha256"],
		Mode:   workspacewrite.EffectiveInstallMode(info.Mode().Perm()),
	}, nil
}

func runtimePathReserved(root, rel string, tracked map[string]bool) bool {
	return tracked[strings.ToLower(rel)] || installPathReserved(root, rel)
}

// workspaceRelative converts an absolute path inside root to a clean
// workspace-relative path.
func workspaceRelative(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return "", false
	}
	return cleanWorkspacePath(rel), true
}

func cleanWorkspacePath(path string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
}

func workspacePathSafe(root, rel string) bool {
	return filepath.IsLocal(filepath.FromSlash(rel)) &&
		workspacewrite.SafePath(root, rel) == nil
}

func isManagedRuntimeOutput(out bootstrap.Output) bool {
	return out.Policy == bootstrap.PolicyManaged && out.Role == bootstrap.RoleRuntime
}

// observedSeedPaths lists the seed outputs a verified bootstrap produced.
// Tracked files stay user-owned and unrecorded.
func observedSeedPaths(root string, outputs []bootstrap.Output, tracked map[string]bool) []string {
	paths := make([]string, 0, len(outputs))
	for _, out := range outputs {
		if out.Policy != bootstrap.PolicySeed {
			continue
		}
		clean := cleanWorkspacePath(out.Path)
		if tracked[strings.ToLower(clean)] || !workspacePathSafe(root, clean) {
			continue
		}
		paths = append(paths, clean)
	}
	slices.Sort(paths)
	return paths
}
