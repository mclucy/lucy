package operation

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/install"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

// runtimeObservation binds the selected runtime identity, membership, and
// regular-file bytes observed before identification performs network work.
// Adoption may only publish these bytes.
type runtimeObservation struct {
	entryPoint  string
	product     serverpkg.Product
	minecraft   version.Version
	coreVersion version.Version
	members     map[string]discovery.FileRole
	files       []install.AdoptedRuntimeFile
}

func captureRuntimeObservation(
	selection *discovery.Selection,
) (*runtimeObservation, error) {
	if selection == nil || selection.Installation == nil {
		return nil, nil
	}
	files, err := adoptedRuntimeFiles(selection)
	if err != nil {
		return nil, err
	}
	members := make(map[string]discovery.FileRole, len(selection.Installation.Files))
	for _, file := range selection.Installation.Files {
		members[file.Path] = file.Role
	}
	entryPoint := ""
	if selection.EntryPoint != nil {
		entryPoint = selection.EntryPoint.Path
	}
	return &runtimeObservation{
		entryPoint:  entryPoint,
		product:     selection.Product(),
		minecraft:   selection.MinecraftVersion(),
		coreVersion: selection.Installation.Identity.Primary.Version,
		members:     members,
		files:       files,
	}, nil
}

// verify rejects a runtime that changed after observation. The caller passes a
// fresh discovery result, and verify rechecks the selected entry point,
// identity, membership roles, and every observed file's digest and mode, so
// inconclusive adoption cannot record ownership of a replacement using stale
// membership.
func (o *runtimeObservation) verify(selection *discovery.Selection) error {
	if o == nil {
		if selection != nil {
			return errors.New("a runtime appeared after observation; rerun lucy init")
		}
		return nil
	}
	if selection == nil || selection.Installation == nil {
		return errors.New("the observed runtime disappeared during initialization")
	}
	entryPoint := ""
	if selection.EntryPoint != nil {
		entryPoint = selection.EntryPoint.Path
	}
	if entryPoint != o.entryPoint ||
		selection.Product() != o.product ||
		selection.MinecraftVersion() != o.minecraft ||
		selection.Installation.Identity.Primary.Version != o.coreVersion {
		return errors.New("the selected runtime identity changed during initialization")
	}
	if !o.sameMembership(selection.Installation.Files) {
		return errors.New("the selected runtime membership changed during initialization")
	}
	for _, observed := range o.files {
		if err := verifyRuntimeFileBaseline(observed); err != nil {
			return err
		}
	}
	return nil
}

func (o *runtimeObservation) sameMembership(files []discovery.File) bool {
	if len(files) != len(o.members) {
		return false
	}
	for _, file := range files {
		role, ok := o.members[file.Path]
		if !ok || role != file.Role {
			return false
		}
	}
	return true
}

func verifyRuntimeFileBaseline(observed install.AdoptedRuntimeFile) error {
	info, err := os.Lstat(observed.Path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf(
			"runtime file %s changed during initialization",
			observed.Path,
		)
	}
	hashes, err := install.DigestFile(observed.Path, nil)
	if err != nil {
		return fmt.Errorf(
			"digest runtime file %s: %w",
			observed.Path,
			err,
		)
	}
	if hashes["sha256"] != observed.ExpectedSHA256 ||
		uint32(info.Mode().Perm()) != observed.ExpectedMode {
		return fmt.Errorf(
			"runtime file %s changed during initialization",
			observed.Path,
		)
	}
	return nil
}

// adoptedRuntimeFiles captures the digest and mode of every file the selected
// runtime owns. Adoption records only files with a captured baseline, so an
// unreadable or replaced file fails initialization instead of being adopted
// without evidence.
func adoptedRuntimeFiles(
	selection *discovery.Selection,
) ([]install.AdoptedRuntimeFile, error) {
	if selection == nil || selection.Installation == nil {
		return nil, nil
	}
	files := make([]install.AdoptedRuntimeFile, 0, len(selection.Installation.Files))
	for _, f := range selection.Installation.Files {
		info, err := os.Lstat(f.Path)
		if err != nil {
			return nil, fmt.Errorf("inspect runtime file %s: %w", f.Path, err)
		}
		if !info.Mode().IsRegular() {
			if info.Mode()&os.ModeSymlink != 0 {
				// A launcher alias stays unowned; discovery selects the
				// canonical target when one exists.
				continue
			}
			return nil, fmt.Errorf("runtime file %s is not a regular file", f.Path)
		}
		hashes, err := install.DigestFile(f.Path, nil)
		if err != nil {
			return nil, fmt.Errorf("digest runtime file %s: %w", f.Path, err)
		}
		files = append(files, install.AdoptedRuntimeFile{
			Path:           f.Path,
			Role:           f.Role,
			ExpectedSHA256: hashes["sha256"],
			ExpectedMode:   uint32(info.Mode().Perm()),
		})
	}
	return files, nil
}

// initResult carries the observations of a failed initialization so the
// command boundary can present them alongside the failure.
func initResult(observed *workspace.Workspace) *InitResult {
	if observed == nil {
		return nil
	}
	return &InitResult{
		Warnings:  observed.Warnings,
		Discovery: observed.Discovery,
	}
}

func concreteVersion(v version.Version) bool {
	return v != "" && !v.IsInvalid() && !v.CanInfer()
}

func paperLineageProduct(product serverpkg.Product) bool {
	switch product {
	case serverpkg.ProductPaper, serverpkg.ProductPurpur, serverpkg.ProductFolia:
		return true
	default:
		return false
	}
}

// identifyInstalledCoreBuild asks the source which build the installed Paper
// or Purpur jar is, using the digest that source identifies builds by.
func (s *Service) identifyInstalledCoreBuild(
	ctx context.Context,
	root string,
	selection *discovery.Selection,
	product serverpkg.Product,
) (version.Version, bool, error) {
	if selection == nil || selection.EntryPoint == nil ||
		selection.EntryPoint.Path == "" {
		return "", false, nil
	}
	hasher, flavor, err := buildDigest(product)
	if err != nil {
		return "", false, err
	}
	path := selection.EntryPoint.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	digest, err := digestFile(hasher, path)
	if err != nil {
		return "", false, fmt.Errorf("read %s jar: %w", product, err)
	}
	var build version.Version
	var found bool
	if product == serverpkg.ProductPurpur {
		build, found, err = s.source.IdentifyPurpurBuild(
			ctx,
			selection.MinecraftVersion(),
			digest,
		)
	} else {
		build, found, err = s.source.IdentifyFillBuild(
			ctx,
			flavor,
			selection.MinecraftVersion(),
			digest,
		)
	}
	if err != nil {
		return "", false, fmt.Errorf("identify installed %s build: %w", product, err)
	}
	return build, found, nil
}

// buildDigest selects the digest algorithm and Fill flavor that identify one
// paper-lineage product.
func buildDigest(product serverpkg.Product) (hash.Hash, string, error) {
	switch product {
	case serverpkg.ProductPaper:
		return sha256.New(), "paper", nil
	case serverpkg.ProductFolia:
		return sha256.New(), "folia", nil
	case serverpkg.ProductPurpur:
		return md5.New(), "", nil
	default:
		return nil, "", fmt.Errorf("product %s has no build digest", product)
	}
}

func digestFile(hasher hash.Hash, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(hasher, file)
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
