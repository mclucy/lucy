package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
)

// adoptPackages claims identified package files that discovery observed. A
// file whose bytes or mode changed after discovery fails adoption instead of
// entering the receipt with a stale baseline.
func adoptPackages(
	files *os.Root,
	root string,
	opts AdoptOptions,
	tracked map[string]bool,
	ledger *adoptionLedger,
	check currentCheck,
) error {
	for _, pkg := range opts.Packages {
		if pkg.Path == "" {
			continue
		}
		fullPath := pkg.Path
		if !filepath.IsAbs(fullPath) {
			fullPath = filepath.Join(root, fullPath)
		}
		cleanRel, ok := workspaceRelative(root, fullPath)
		if !ok {
			return fmt.Errorf("package file %s escapes the workspace", pkg.Path)
		}
		if err := workspacewrite.SafePath(root, cleanRel); err != nil {
			return err
		}
		if tracked[strings.ToLower(cleanRel)] || installPathReserved(root, cleanRel) {
			continue
		}
		image, err := workspacewrite.ObserveInstallImageCached(files, cleanRel, check.memo)
		if err != nil || image == nil {
			if pkg.ExpectedSHA256 != "" {
				if err != nil {
					return fmt.Errorf("inspect accepted package file %s: %w", cleanRel, err)
				}
				return fmt.Errorf(
					"package file %s disappeared between discovery and adoption publication",
					cleanRel,
				)
			}
			if opts.Logger != nil {
				opts.Logger.Debug(
					"skip package file without an observed image",
					"path", cleanRel,
				)
			}
			continue
		}
		if pkg.ExpectedSHA256 != "" {
			if !strings.EqualFold(image.SHA256, pkg.ExpectedSHA256) {
				return fmt.Errorf(
					"package file %s changed between discovery and adoption publication",
					cleanRel,
				)
			}
			if image.Mode != workspacewrite.EffectiveInstallMode(os.FileMode(pkg.ExpectedMode)) {
				return fmt.Errorf(
					"package file %s changed mode between discovery and adoption publication",
					cleanRel,
				)
			}
		}

		packageID := pkg.PackageID
		if packageID == "" {
			packageID = lockedPackageID(opts.Lock, cleanRel)
		}
		if err := ledger.claim(
			cleanRel,
			receipt.OriginPackage,
			packageID,
			image,
		); err != nil {
			return err
		}
	}
	return nil
}

// lockedPackageID returns the identity installation records for the package
// whose destination is path.
func lockedPackageID(lock *document.Lock, path string) string {
	if lock == nil {
		return ""
	}
	for i := range lock.Packages {
		pkg := &lock.Packages[i]
		dest, err := lock.PackageDestination(pkg)
		if err != nil || cleanWorkspacePath(dest) != path {
			continue
		}
		if pkg.Artifact.ProjectID != "" {
			return pkg.Artifact.Provider.String() + ":" + pkg.Artifact.ProjectID
		}
		return pkg.Filename
	}
	return ""
}

// adoptMCDRWrapper claims the MCDR wrapper launcher when installation would
// generate exactly those bytes. Initialization selects no isolated
// environment, so a customized or venv-specific wrapper stays unowned.
func adoptMCDRWrapper(
	root string,
	opts AdoptOptions,
	tracked map[string]bool,
	ledger *adoptionLedger,
) error {
	if opts.Lock.MCDR == nil {
		return nil
	}
	relative := cleanWorkspacePath(
		filepath.Join(opts.Lock.MCDR.Directory, "run-mcdr.sh"),
	)
	if !workspacePathSafe(root, relative) ||
		tracked[strings.ToLower(relative)] ||
		installPathReserved(root, relative) {
		return nil
	}

	target := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	matched, err := matchesGeneratedWrapper(root, opts.Lock, data)
	if err != nil {
		return err
	}
	if !matched {
		if opts.Logger != nil {
			opts.Logger.Debug(
				"MCDR wrapper launcher left unowned; bytes do not match a generated launcher",
				"path", relative,
			)
		}
		return nil
	}
	return ledger.claim(
		relative,
		receipt.OriginMCDR,
		"",
		workspacewrite.InstallBytesImage(data, info.Mode().Perm()),
	)
}

// matchesGeneratedWrapper reports whether data equals one of the launcher
// scripts installation can generate for the lock. An isolated launcher is only
// valid while the interpreter it names exists in the workspace.
func matchesGeneratedWrapper(
	root string,
	lock *document.Lock,
	data []byte,
) (bool, error) {
	for _, variant := range mcdrLauncherVariants(lock) {
		if !bytes.Equal(data, variant.Script) {
			continue
		}
		if variant.Interpreter == "" {
			return true, nil
		}
		if !workspacePathSafe(root, variant.Interpreter) {
			continue
		}
		info, err := os.Lstat(
			filepath.Join(root, filepath.FromSlash(variant.Interpreter)),
		)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		return true, nil
	}
	return false, nil
}
