package bootstrap

import (
	"archive/zip"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
)

// forgeDeclaredFile is one runtime file whose digest an installer declares.
// The digest lives inside a pinned artifact, so a matching file needs no
// network or execution to be proven.
type forgeDeclaredFile struct {
	Algorithm string
	Digest    string
	Label     string
	// Optional files may be absent from the tree (for example installer
	// launch data that a later generation no longer writes).
	Optional bool
}

// forgeDeclaredIntegrity extracts the files an installer declares with a
// digest. It reads the pinned installer and never executes it. Paths that are
// only staging material are omitted because a committed runtime tree does not
// contain them.
func forgeDeclaredIntegrity(
	serverDir, installer string,
	locked document.LockedServer,
) (map[string]forgeDeclaredFile, error) {
	z, err := zip.OpenReader(installer)
	if err != nil {
		return nil, fmt.Errorf("open pinned installer: %w", err)
	}
	defer z.Close()
	profile, err := readForgeProfile(&z.Reader)
	if err != nil {
		return nil, err
	}
	vars, err := forgeVars(serverDir, installer, locked, profile.Data)
	if err != nil {
		return nil, err
	}
	declared := map[string]forgeDeclaredFile{}
	add := func(dest, algorithm, digest, label string, optional bool) {
		rel, ok := serverRelativePath(serverDir, dest)
		if !ok || isForgeDiscardInput(rel) {
			return
		}
		declared[rel] = forgeDeclaredFile{
			Algorithm: algorithm,
			Digest:    strings.ToLower(digest),
			Label:     label,
			Optional:  optional,
		}
	}
	if profile.Path != "" {
		rel, err := safeForgeMavenPath(profile.Path)
		if err != nil {
			return nil, fmt.Errorf("installer profile artifact: %w", err)
		}
		raw, found, err := fsutil.ReadZipEntry(&z.Reader, "maven/"+rel, fsutil.MaxNestedZipEntryBytes)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("installer profile artifact maven/%s is missing", rel)
		}
		sum := sha256.Sum256(raw)
		add(
			filepath.Join(serverDir, filepath.Base(rel)),
			"sha256", hex.EncodeToString(sum[:]),
			"installer profile artifact", false,
		)
	}
	for _, proc := range profile.Processors {
		if !serverSideProcessor(proc.Sides) {
			continue
		}
		for name, expected := range proc.Outputs {
			dest, err := expandForgeValue(serverDir, vars, name)
			if err != nil {
				return nil, err
			}
			want, err := expandForgeValue(serverDir, vars, expected)
			if err != nil {
				return nil, err
			}
			if !isSHA1Hex(want) {
				continue
			}
			add(dest, "sha1", want, "installer processor output", false)
		}
		if slices.Contains(proc.Args, "EXTRACT_FILES") {
			addExtractedFiles(&z.Reader, serverDir, vars, proc.Args, add)
		}
	}
	for key, value := range profile.Data {
		if !strings.HasSuffix(key, "_SHA") {
			continue
		}
		base, ok := profile.Data[strings.TrimSuffix(key, "_SHA")]
		if !ok {
			continue
		}
		dest, err := expandForgeValue(serverDir, vars, base.Server)
		if err != nil {
			return nil, err
		}
		want, err := expandForgeValue(serverDir, vars, value.Server)
		if err != nil {
			return nil, err
		}
		if !isSHA1Hex(want) {
			continue
		}
		add(dest, "sha1", want, "installer-declared output", false)
	}
	return declared, nil
}

// addExtractedFiles declares the installer data files an EXTRACT_FILES
// processor writes. The expected bytes are the pinned installer's own entries.
func addExtractedFiles(
	z *zip.Reader,
	serverDir string,
	vars map[string]string,
	args []string,
	add func(dest, algorithm, digest, label string, optional bool),
) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] != "--from" {
			continue
		}
		source := args[i+1]
		end := -1
		for j := i + 2; j+1 < len(args); j++ {
			if args[j] == "--to" {
				end = j
				break
			}
		}
		if end < 0 {
			return
		}
		dest, err := expandForgeValue(serverDir, vars, args[end+1])
		if err != nil {
			return
		}
		raw, found, err := fsutil.ReadZipEntry(z, source, fsutil.MaxNestedZipEntryBytes)
		if err != nil || !found {
			continue
		}
		sum := sha256.Sum256(raw)
		add(dest, "sha256", hex.EncodeToString(sum[:]), "installer data file", true)
	}
}

// verifyForgeDeclared checks one declared file against its digest and records
// the bytes as installer-declared evidence.
func (v *treeVerifier) verifyForgeDeclared(rel string, declared forgeDeclaredFile) error {
	if err := checkPathSafety(v.serverDir, rel, true); err != nil {
		return err
	}
	path := filepath.Join(v.serverDir, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && declared.Optional {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("missing %s %s", declared.Label, rel)
	}
	sha1Hex, sha256Hex, err := fileEvidenceDigests(path)
	if err != nil {
		return err
	}
	actual := sha1Hex
	if declared.Algorithm == "sha256" {
		actual = sha256Hex
	}
	if !strings.EqualFold(actual, declared.Digest) {
		return fmt.Errorf(
			"%s %s %s mismatch: got %s, want %s",
			declared.Label, rel, declared.Algorithm, actual, declared.Digest,
		)
	}
	out := v.output(rel, sha256Hex, uint32(info.Mode().Perm()), PolicyManaged)
	out.Evidence = EvidenceInstallerDeclared
	v.add(out)
	return nil
}

// unprovenForgeOutput returns the first file under a runtime library scope that
// no lock pin or installer declaration covers. Such a file prevents a
// currentness claim because the tree cannot be proven complete.
func (v *treeVerifier) unprovenForgeOutput(proven map[string]bool) (string, error) {
	root := filepath.Join(v.serverDir, "libraries")
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "libraries", nil
	}
	unproven := ""
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(v.serverDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if proven[rel] {
			return nil
		}
		unproven = rel
		return filepath.SkipAll
	})
	return unproven, err
}

// fileEvidenceDigests computes the SHA-1 and SHA-256 digests of one file in a
// single read, so declared evidence costs one pass per file.
func fileEvidenceDigests(path string) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	sha1Hasher, sha256Hasher := sha1.New(), sha256.New()
	if _, err := io.Copy(io.MultiWriter(sha1Hasher, sha256Hasher), file); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(sha1Hasher.Sum(nil)), hex.EncodeToString(sha256Hasher.Sum(nil)), nil
}

func isSHA1Hex(value string) bool {
	if len(value) != sha1.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func serverRelativePath(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
