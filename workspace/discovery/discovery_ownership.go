package discovery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const ownershipReceiptPath = ".lucy/install.yaml"

type ownershipReceipt struct {
	Version int                     `yaml:"version"`
	Entries []ownershipReceiptEntry `yaml:"entries"`
}

type ownershipReceiptEntry struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
}

// ObserveOwnership verifies the local installation receipt against every
// observed member. Receipt paths are workspace-anchor-relative even when the
// server directory is nested. A recorded path alone never proves management:
// the content hash must match. A member whose recorded content cannot be read
// keeps OwnershipUnknown and never counts as modified. The method never writes
// files.
func (r *Result) ObserveOwnership(workspaceRoot string) {
	if r == nil {
		return
	}
	r.clearOwnershipDiagnostics()
	for index := range r.Installations {
		for fileIndex := range r.Installations[index].Files {
			r.Installations[index].Files[fileIndex].Ownership = OwnershipUnmanaged
		}
	}

	absoluteRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		r.appendReceiptError(workspaceRoot, err)
		return
	}
	root, err := os.OpenRoot(absoluteRoot)
	if err != nil {
		r.appendReceiptError(absoluteRoot, err)
		return
	}
	defer root.Close()

	data, err := root.ReadFile(ownershipReceiptPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		r.appendReceiptError(filepath.Join(absoluteRoot, ownershipReceiptPath), err)
		return
	}
	var receipt ownershipReceipt
	if err := yaml.Unmarshal(data, &receipt); err != nil {
		r.appendReceiptError(
			filepath.Join(absoluteRoot, ownershipReceiptPath),
			fmt.Errorf("parse installation receipt: %w", err),
		)
		return
	}
	if receipt.Version != 1 {
		r.appendReceiptError(
			filepath.Join(absoluteRoot, ownershipReceiptPath),
			fmt.Errorf("unsupported installation receipt version %d", receipt.Version),
		)
		return
	}

	// Duplicate receipt entries are kept: a path may carry several recorded
	// hashes, and any match proves the observed content.
	recorded := make(map[string][]string, len(receipt.Entries))
	for _, entry := range receipt.Entries {
		relative := filepath.Clean(filepath.FromSlash(entry.Path))
		if !filepath.IsLocal(relative) || relative == "." {
			continue
		}
		hash := normalizeHash(entry.SHA256)
		recorded[relative] = append(recorded[relative], hash)
		if resolved, resolveErr := filepath.EvalSymlinks(
			filepath.Join(absoluteRoot, relative),
		); resolveErr == nil {
			if target, ok := relativeWithinRoot(absoluteRoot, resolved); ok &&
				target != relative {
				recorded[target] = append(recorded[target], hash)
			}
		}
	}

	verified := make(map[string]verifiedHash, len(recorded))
	for index := range r.Installations {
		installation := &r.Installations[index]
		for fileIndex := range installation.Files {
			file := &installation.Files[fileIndex]
			relative, ok := relativeWithinRoot(absoluteRoot, file.Path)
			if !ok {
				continue
			}
			hashes, listed := recorded[relative]
			if !listed {
				continue
			}
			observed, ok := verified[relative]
			if !ok {
				hash, hashErr := hashRootFile(root, relative)
				observed = verifiedHash{hash: hash, err: hashErr}
				verified[relative] = observed
			}
			if observed.err != nil {
				file.Ownership = OwnershipUnknown
				r.Diagnostics = append(r.Diagnostics, Finding{
					Kind:     FindingOwnershipError,
					Path:     file.Path,
					Identity: installation.Identity,
					Cause:    observed.err,
				})
				continue
			}
			if matchesRecordedHash(observed.hash, hashes) {
				file.Ownership = OwnershipManaged
				continue
			}
			file.Ownership = OwnershipModified
			r.Diagnostics = append(r.Diagnostics, Finding{
				Kind:     FindingModifiedFile,
				Path:     file.Path,
				Identity: installation.Identity,
			})
		}
	}
}

type verifiedHash struct {
	hash string
	err  error
}

// relativeWithinRoot resolves a member path against the workspace anchor even
// when one side uses a symlinked spelling. It returns a path relative to the
// anchor spelling first, then to the resolved anchor.
func relativeWithinRoot(root, path string) (string, bool) {
	roots := []string{root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
		roots = append(roots, resolved)
	}
	paths := []string{path}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != path {
		paths = append(paths, resolved)
	}
	for _, candidateRoot := range roots {
		for _, candidatePath := range paths {
			rel, err := filepath.Rel(candidateRoot, candidatePath)
			if err == nil && rel != "." && filepath.IsLocal(rel) {
				return rel, true
			}
		}
	}
	return "", false
}

func hashRootFile(root *os.Root, relative string) (string, error) {
	info, err := root.Stat(relative)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errNotRegularFile
	}
	file, err := root.Open(relative)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func matchesRecordedHash(observed string, recorded []string) bool {
	for _, expected := range recorded {
		if expected != "" && observed == expected {
			return true
		}
	}
	return false
}

func normalizeHash(hash string) string {
	return strings.ToLower(strings.TrimSpace(hash))
}

func (r *Result) appendReceiptError(path string, err error) {
	if r == nil {
		return
	}
	r.Diagnostics = append(r.Diagnostics, Finding{
		Kind:  FindingReceiptError,
		Path:  path,
		Cause: err,
	})
}

func (r *Result) clearOwnershipDiagnostics() {
	if r == nil || len(r.Diagnostics) == 0 {
		return
	}
	kept := r.Diagnostics[:0]
	for _, finding := range r.Diagnostics {
		switch finding.Kind {
		case FindingReceiptError, FindingOwnershipError, FindingModifiedFile:
			continue
		}
		kept = append(kept, finding)
	}
	clear(r.Diagnostics[len(kept):])
	r.Diagnostics = kept
}
