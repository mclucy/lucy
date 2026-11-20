package bootstrap

import (
	"archive/zip"
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// verifyPaperclip verifies the pinned Paperclip core and the metadata lists
// that describe the files it extracts beside it. The core may sit under the
// staged name or under the name an imported archive gave it; its pinned digest
// decides, never its filename.
func (v *treeVerifier) verifyPaperclip() CheckResult {
	coreRel, ok := v.paperclipCore()
	v.verifyInputs()

	if !v.failed && ok {
		zr, err := zip.OpenReader(filepath.Join(v.serverDir, filepath.FromSlash(coreRel)))
		if err != nil {
			v.fail("opening Paperclip core %s: %v", coreRel, err)
		} else {
			defer zr.Close()
			if reason := v.verifyPaperclipMetadata(&zr.Reader); reason != "" {
				return v.result(CurrentnessInconclusive, reason)
			}
		}
	}
	if v.failed {
		return v.mismatch()
	}
	return v.result(CurrentnessCurrent, "")
}

// paperclipCore locates the locked core artifact in the tree. The staged name
// is tried first, then the locked filename, then any root file whose bytes
// match the locked digests. A file under a candidate name that fails its
// digests is a mismatch, not a reason to keep searching.
func (v *treeVerifier) paperclipCore() (string, bool) {
	candidates := []string{"server.jar"}
	if name := filepath.ToSlash(filepath.Clean(v.locked.Filename)); name != "" &&
		name != "." && filepath.IsLocal(filepath.FromSlash(name)) &&
		!slices.Contains(candidates, name) {
		candidates = append(candidates, name)
	}
	for _, rel := range candidates {
		path := filepath.Join(v.serverDir, filepath.FromSlash(rel))
		info, err := os.Lstat(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				v.fail("inspecting Paperclip core %s: %v", rel, err)
				return "", false
			}
			continue
		}
		if !info.Mode().IsRegular() {
			v.fail("Paperclip core %s is not a regular file", rel)
			return "", false
		}
		out, ok := v.verifyPinned(rel, v.locked.Artifact.Hashes)
		if !ok {
			return "", false
		}
		out.Policy, out.Role = PolicyManaged, RoleRuntime
		v.add(out)
		return rel, true
	}
	entries, err := os.ReadDir(v.serverDir)
	if err != nil {
		v.fail("reading server directory for the Paperclip core: %v", err)
		return "", false
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || slices.Contains(candidates, entry.Name()) {
			continue
		}
		digest, mode, matches := v.matchesPinned(entry.Name(), v.locked.Artifact.Hashes)
		if !matches {
			continue
		}
		out := v.output(entry.Name(), digest, mode, "")
		out.Policy, out.Role = PolicyManaged, RoleRuntime
		out.Evidence = EvidenceLocked
		v.add(out)
		return entry.Name(), true
	}
	v.fail("locked Paperclip core artifact %s is not present at the server root", v.locked.Filename)
	return "", false
}

// verifyPaperclipMetadata verifies the entries Paperclip extracts from its
// embedded lists. A missing libraries.list cannot prove the extracted tree
// complete, so it reports an inconclusive reason instead of a mismatch.
func (v *treeVerifier) verifyPaperclipMetadata(z *zip.Reader) string {
	if err := v.verifyPaperclipCache(z); err != nil {
		v.fail("%v", err)
	}
	if err := v.verifyPaperclipVersions(z); err != nil {
		v.fail("%v", err)
	}
	patched, err := v.verifyPaperclipPatches(z)
	if err != nil {
		v.fail("%v", err)
	}
	if v.failed {
		return ""
	}

	libBytes, err := readZipEntry(z, "META-INF/libraries.list")
	if err != nil {
		return "missing META-INF/libraries.list in Paperclip jar"
	}
	scanner := bufio.NewScanner(bytes.NewReader(libBytes))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			v.fail("malformed line in libraries.list: %q", line)
			break
		}
		expectedHash := parts[0]
		libRel, err := scopedPath("libraries", parts[2])
		if err != nil {
			v.fail("libraries.list path safety: %v", err)
			break
		}
		// A patched library was replaced after extraction, so its unpatched
		// hash no longer describes the file.
		if patched[libRel] {
			continue
		}
		if err := v.verifyKnownOutput(libRel, expectedHash, "Paperclip library output"); err != nil {
			v.fail("%v", err)
		}
	}
	if err := scanner.Err(); err != nil && !v.failed {
		v.fail("reading libraries.list: %v", err)
	}
	return ""
}

func (v *treeVerifier) verifyPaperclipCache(z *zip.Reader) error {
	raw, err := readZipEntry(z, "META-INF/download-context")
	if err != nil {
		return fmt.Errorf("missing download-context in Paperclip jar")
	}
	parts := strings.Fields(string(raw))
	if len(parts) != 3 {
		return fmt.Errorf("malformed download-context")
	}
	cacheRel, err := scopedPath("cache", parts[2])
	if err != nil {
		return fmt.Errorf("download-context scope safety: %v", err)
	}
	return v.verifyKnownOutput(cacheRel, parts[0], "Paperclip cache file")
}

func (v *treeVerifier) verifyPaperclipVersions(z *zip.Reader) error {
	raw, err := readZipEntry(z, "META-INF/versions.list")
	if err != nil {
		return fmt.Errorf("missing META-INF/versions.list in Paperclip jar")
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 3 {
			return fmt.Errorf("malformed line in versions.list: %q", line)
		}
		versionRel, err := scopedPath("versions", parts[2])
		if err != nil {
			return fmt.Errorf("versions.list path safety: %v", err)
		}
		if err := v.verifyKnownOutput(versionRel, parts[0], "Paperclip version"); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading versions.list: %v", err)
	}
	return nil
}

// verifyPaperclipPatches verifies the files patches.list replaces and returns
// their paths, because libraries.list still lists the unpatched files.
func (v *treeVerifier) verifyPaperclipPatches(z *zip.Reader) (map[string]bool, error) {
	patched := map[string]bool{}
	raw, err := readZipEntry(z, "META-INF/patches.list")
	if err != nil {
		// Patching is optional; a jar without the list has no patched files.
		return patched, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) != 7 {
			return patched, fmt.Errorf("malformed line in patches.list: %q", line)
		}
		location := parts[0]
		if location != "versions" && location != "libraries" {
			return patched, fmt.Errorf(
				"invalid patch location %q in patches.list (must be versions or libraries)",
				location,
			)
		}
		outRel, err := scopedPath(location, parts[6])
		if err != nil {
			return patched, fmt.Errorf("patches.list path safety: %v", err)
		}
		patched[outRel] = true
		if err := v.verifyKnownOutput(outRel, parts[3], "Paperclip patched output"); err != nil {
			return patched, err
		}
	}
	if err := scanner.Err(); err != nil {
		return patched, fmt.Errorf("reading patches.list: %v", err)
	}
	return patched, nil
}

// verifyKnownOutput verifies one extracted file whose digest is embedded in
// the server.jar metadata rather than in the lock.
func (v *treeVerifier) verifyKnownOutput(rel, expectedSHA256, label string) error {
	if err := checkPathSafety(v.serverDir, rel, true); err != nil {
		return err
	}
	path := filepath.Join(v.serverDir, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("missing %s %s", label, rel)
	}
	actual := v.cached[rel]
	if actual == "" {
		actual, err = fileSHA256(path)
	}
	if err != nil || !strings.EqualFold(actual, expectedSHA256) {
		return fmt.Errorf("%s %s SHA256 mismatch", label, rel)
	}
	out := v.output(rel, actual, uint32(info.Mode().Perm()), PolicyManaged)
	out.Evidence = EvidenceInstallerDeclared
	v.add(out)
	return nil
}
