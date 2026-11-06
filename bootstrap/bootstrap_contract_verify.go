package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
)

// ValidateStagedOutputs inspects the staged server tree under stageRoot/locked.Directory.
// It verifies that:
// - All paths are safe relative paths within the server directory without symlink or FIFO escapes.
// - All locked inputs and runtime core artifacts match their expected hashes.
// - Every file falls within the method's declared exact paths or bounded scopes.
// - No collisions exist between output policies or origins.
// - Method launcher semantics match.
// Discard and bootstrap-only files (e.g. Forge .bootstrap-data/, server-mappings.txt) are preserved in the result
// with PolicyDiscard / RoleBootstrapOnly so callers omit them from committed runtime receipts.
func ValidateStagedOutputs(stageRoot string, locked document.LockedServer) ([]Output, error) {
	exact, scopes, err := validateContract(locked)
	if err != nil {
		return nil, fmt.Errorf("invalid bootstrap output contract: %w", err)
	}

	if err := checkPathSafety(stageRoot, locked.Directory, false); err != nil {
		return nil, fmt.Errorf("unsafe server directory %q: %w", locked.Directory, err)
	}
	serverRoot := filepath.Join(stageRoot, locked.Directory)
	if _, err := os.Stat(serverRoot); err != nil {
		return nil, fmt.Errorf("staged server directory does not exist: %w", err)
	}

	var outputs []Output
	seen := make(map[string]bool)
	cachedDigests := make(map[string]string)

	err = filepath.WalkDir(serverRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == serverRoot {
			return nil
		}
		rel, err := filepath.Rel(serverRoot, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))

		if d.IsDir() {
			return nil
		}

		if err := checkPathSafety(serverRoot, rel, true); err != nil {
			return err
		}

		if seen[rel] {
			return fmt.Errorf("duplicate staged output: %s", rel)
		}
		seen[rel] = true

		var policy OutputPolicy
		var role MaterialRole

		if spec, ok := exact[rel]; ok {
			policy = spec.Policy
			role = spec.Role
		} else {
			matchedScope := false
			for _, sc := range scopes {
				if strings.HasPrefix(rel, sc.Scope) {
					policy = sc.Policy
					role = sc.Role
					matchedScope = true
					break
				}
			}
			if !matchedScope {
				return fmt.Errorf("unpermitted output file %q outside declared bootstrap scope", rel)
			}
		}

		f, err := os.Open(p)
		if err != nil {
			return err
		}
		hasher := sha256.New()
		if _, err := io.Copy(hasher, f); err != nil {
			f.Close()
			return err
		}
		f.Close()
		digest := hex.EncodeToString(hasher.Sum(nil))
		cachedDigests[rel] = digest

		info, err := d.Info()
		if err != nil {
			return err
		}

		outputs = append(outputs, Output{
			Path:       workspacePath(locked, rel),
			ServerPath: rel,
			SHA256:     digest,
			Mode:       uint32(info.Mode().Perm()),
			Policy:     policy,
			Role:       role,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Shared method verification for staging: verify required outputs, pinned hashes, and launcher semantics.
	// cachedDigests avoids re-reading and re-hashing all files from disk a second time.
	verification, err := newTreeVerifier(serverRoot, locked, exact, true, cachedDigests).verify()
	if err != nil {
		return nil, fmt.Errorf("staged bootstrap verification error: %w", err)
	}
	if !verification.Current {
		return nil, fmt.Errorf("staged bootstrap output failed verification: %s", verification.Reason)
	}

	slices.SortFunc(outputs, func(a, b Output) int {
		return strings.Compare(a.Path, b.Path)
	})
	return outputs, nil
}

// InspectStage is an alias for ValidateStagedOutputs.
func InspectStage(stageRoot string, locked document.LockedServer) ([]Output, error) {
	return ValidateStagedOutputs(stageRoot, locked)
}

// CheckExisting checks whether an existing server directory already satisfies the locked bootstrap contract
// without executing bootstrap.
// It shares the single verification path with stage validation, verifying:
// - Exact presence and hashes of runtime locked inputs and runtime core artifacts. Stage-only inputs
// (Forge's bootstrap-only server.jar, server-mappings.txt, and .bootstrap-data/) are consumed by installer
// execution and absent from a committed tree, so they are neither required nor verified here; staging still verifies them.
// - Semantics and classpath of generated launch files.
// - Embedded metadata (download-context, versions.list, patches.list, libraries.list) for Paperclip.
// For methods where outputs are generated dynamically and cannot be proven identical to lock artifacts
// without execution (such as Forge installer processors), CheckExisting returns
// CurrentnessInconclusive and Current=false. Inconclusive must never be reported as success.
// When verification is inconclusive but not conflicting, verified attributable outputs are returned
// in Outputs so adoption can preserve them without claiming full bootstrap currentness.
// Definite conflicts and hash mismatches return CurrentnessMismatch.
func CheckExisting(serverDir string, locked document.LockedServer) (CheckResult, error) {
	exact, _, err := validateContract(locked)
	if err != nil {
		return CheckResult{
			Current: false,
			Status:  CurrentnessMismatch,
			Reason:  fmt.Sprintf("invalid bootstrap contract: %v", err),
		}, err
	}
	return newTreeVerifier(serverDir, locked, exact, false, nil).verify()
}
