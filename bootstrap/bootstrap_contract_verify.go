package bootstrap

import (
	"fmt"
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
	tree, err := contractTreeOutputs(stageRoot, locked)
	if err != nil {
		return nil, err
	}
	// cached digests avoid re-reading and re-hashing all files from disk a second time.
	verification, err := newTreeVerifier(
		tree.serverRoot, locked, tree.exact, true, tree.cached, tree.launcher, "",
	).verify()
	if err != nil {
		return nil, fmt.Errorf("staged bootstrap verification error: %w", err)
	}
	if !verification.Current {
		return nil, fmt.Errorf("staged bootstrap output failed verification: %s", verification.Reason)
	}
	evidence := make(map[string]Evidence, len(verification.Outputs))
	for _, output := range verification.Outputs {
		evidence[output.ServerPath] = output.Evidence
	}
	for i := range tree.outputs {
		output := &tree.outputs[i]
		output.Evidence = evidence[output.ServerPath]
		if output.Evidence == "" && output.Role == RoleRuntime && output.Policy != PolicyDiscard {
			output.Evidence = EvidenceExecuted
		}
	}
	return tree.outputs, nil
}

// ValidateSnapshotOutputs classifies a runtime tree restored from a snapshot.
// A snapshot stores only the runtime and seed zones, so stage-only inputs are
// absent by design and the completeness of installer-declared outputs was
// already proven when the snapshot was published. A definite conflict still
// fails: the restored tree must satisfy the contract's pinned inputs, launcher
// semantics, and path scopes. coreFile carries the same read-only installer
// evidence the existing-tree check uses, so a tampered declared output is a
// mismatch rather than an accepted restore.
func ValidateSnapshotOutputs(stageRoot string, locked document.LockedServer, coreFile string) ([]Output, error) {
	tree, err := contractTreeOutputs(stageRoot, locked)
	if err != nil {
		return nil, err
	}
	verification, err := newTreeVerifier(
		tree.serverRoot, locked, tree.exact, false, tree.cached, tree.launcher, coreFile,
	).verify()
	if err != nil {
		return nil, fmt.Errorf("restored bootstrap verification error: %w", err)
	}
	if verification.Status == CurrentnessMismatch {
		return nil, fmt.Errorf("restored bootstrap output failed verification: %s", verification.Reason)
	}
	return tree.outputs, nil
}

// contractTree is the classified content of one server tree: the outputs the
// commit needs, the digests already computed while walking, and the launcher
// proof.
type contractTree struct {
	serverRoot string
	exact      map[string]OutputSpec
	launcher   launcherCheck
	outputs    []Output
	cached     map[string]string
}

// contractTreeOutputs validates the contract and walks the server tree under
// stageRoot. Every file must fall in a declared exact path or bounded scope.
// Discard and bootstrap-only files stay in the result so callers can omit them
// from committed runtime receipts.
func contractTreeOutputs(stageRoot string, locked document.LockedServer) (contractTree, error) {
	exact, scopes, err := validateContract(locked)
	if err != nil {
		return contractTree{}, fmt.Errorf("invalid bootstrap output contract: %w", err)
	}
	if err := checkPathSafety(stageRoot, locked.Directory, false); err != nil {
		return contractTree{}, fmt.Errorf("unsafe server directory %q: %w", locked.Directory, err)
	}
	serverRoot := filepath.Join(stageRoot, locked.Directory)
	if _, err := os.Stat(serverRoot); err != nil {
		return contractTree{}, fmt.Errorf("staged server directory does not exist: %w", err)
	}
	// The launcher proves whether run.sh invokes the root shim. Only that proof
	// admits the shim into the contract, so unrelated root files stay unpermitted.
	launcher := verifyLauncher(serverRoot, locked)
	if launcher.rootShim != "" {
		exact[launcher.rootShim] = OutputSpec{
			Path:   launcher.rootShim,
			Policy: PolicyManaged,
			Role:   RoleRuntime,
			Origin: OriginGenerated,
		}
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
		digest, err := fileSHA256(p)
		if err != nil {
			return err
		}
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
		return contractTree{}, err
	}
	slices.SortFunc(outputs, func(a, b Output) int {
		return strings.Compare(a.Path, b.Path)
	})
	return contractTree{
		serverRoot: serverRoot,
		exact:      exact,
		launcher:   launcher,
		outputs:    outputs,
		cached:     cachedDigests,
	}, nil
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
	return Check(serverDir, locked, "")
}

// Check verifies an existing or restored server tree against the locked
// bootstrap contract. coreFile, when supplied, is the materialized core
// artifact the lock pins: a Forge installer that declares processor outputs or
// a Paperclip jar that names its extracted libraries differently from the
// staged layout. It is read-only evidence and may be empty; without it a check
// proves only what the lock pins and the generated launchers declare.
func Check(serverDir string, locked document.LockedServer, coreFile string) (CheckResult, error) {
	exact, _, err := validateContract(locked)
	if err != nil {
		return CheckResult{
			Current: false,
			Status:  CurrentnessMismatch,
			Reason:  fmt.Sprintf("invalid bootstrap contract: %v", err),
		}, err
	}
	// Existing trees prove a root shim against the same canonical copy that staging uses.
	return newTreeVerifier(
		serverDir, locked, exact, false, nil,
		verifyLauncher(serverDir, locked), coreFile,
	).verify()
}
