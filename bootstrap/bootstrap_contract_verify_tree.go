package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
)

// treeVerifier accumulates verified outputs and the first failure reason while
// checking one server tree against its locked bootstrap contract. Stage and
// existing-tree verification share it so both accept exactly the same files.
type treeVerifier struct {
	serverDir string
	locked    document.LockedServer
	contract  map[string]OutputSpec
	staging   bool
	cached    map[string]string
	outputs   []Output
	seen      map[string]bool
	reason    string
	failed    bool
}

func newTreeVerifier(
	serverDir string,
	locked document.LockedServer,
	contract map[string]OutputSpec,
	staging bool,
	cachedDigests map[string]string,
) *treeVerifier {
	return &treeVerifier{
		serverDir: serverDir,
		locked:    locked,
		contract:  contract,
		staging:   staging,
		cached:    cachedDigests,
		seen:      make(map[string]bool),
	}
}

func (v *treeVerifier) verify() (CheckResult, error) {
	if err := checkPathSafety(v.serverDir, "", false); err != nil {
		return CheckResult{
			Current: false,
			Status:  CurrentnessMismatch,
			Reason:  fmt.Sprintf("unsafe server directory: %v", err),
		}, err
	}

	info, err := os.Stat(v.serverDir)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{
				Current: false,
				Status:  CurrentnessMismatch,
				Reason:  "server directory does not exist",
			}, nil
		}
		return CheckResult{
			Current: false,
			Status:  CurrentnessMismatch,
			Reason:  err.Error(),
		}, err
	}
	if !info.IsDir() {
		return CheckResult{
			Current: false,
			Status:  CurrentnessMismatch,
			Reason:  "server path is not a directory",
		}, nil
	}

	var result CheckResult
	switch v.locked.Bootstrapper {
	case serverpkg.BootstrapMethodPlain:
		result = v.verifyPlain()
	case serverpkg.BootstrapMethodPaperclip:
		result = v.verifyPaperclip()
	case serverpkg.BootstrapMethodFabric:
		result = v.verifyFabric()
	case serverpkg.BootstrapMethodForgeInstaller:
		result = v.verifyForgeInstaller()
	default:
		result = CheckResult{
			Current: false,
			Status:  CurrentnessInconclusive,
			Reason:  fmt.Sprintf("unsupported bootstrap adapter %q", v.locked.Bootstrapper),
		}
	}
	return result, nil
}

func (v *treeVerifier) verifyPlain() CheckResult {
	if out, ok := v.verifyPinned("server.jar", v.locked.Artifact.Hashes); ok {
		out.Policy, out.Role = PolicyManaged, RoleRuntime
		v.add(out)
	}
	v.verifyInputs()
	if v.failed {
		return v.mismatch()
	}
	return v.result(CurrentnessCurrent, "")
}

func (v *treeVerifier) verifyFabric() CheckResult {
	loaderRel, err := fabricLoaderPath(v.locked.CoreVersion.String())
	if err != nil {
		v.fail("resolving Fabric loader path: %v", err)
	} else if out, ok := v.verifyPinned(loaderRel, v.locked.Artifact.Hashes); ok {
		out.Policy, out.Role = PolicyManaged, RoleRuntime
		v.add(out)
	}
	v.verifyInputs()

	if err := verifyLauncherSemantics(v.serverDir, v.locked); err != nil {
		v.fail("%v", err)
	} else {
		for _, rel := range []string{
			"run.sh",
			"run.bat",
			"fabric-server-launcher.properties",
			"fabric-server-launch.jar",
		} {
			v.collectRequiredOutput(rel, PolicyManaged)
		}
	}
	if v.failed {
		return v.mismatch()
	}
	return v.result(CurrentnessCurrent, "")
}

func (v *treeVerifier) verifyForgeInstaller() CheckResult {
	v.verifyInputs()

	if err := verifyLauncherSemantics(v.serverDir, v.locked); err != nil {
		v.fail("%v", err)
	} else {
		v.collectRequiredOutput("run.sh", PolicyManaged)
		v.collectRequiredOutput("run.bat", PolicyManaged)
		v.collectRequiredOutput("user_jvm_args.txt", PolicySeed)
		v.collectOptionalOutput("README.txt", PolicySeed)
	}
	if v.failed {
		return v.mismatch()
	}
	if v.staging {
		return v.result(CurrentnessCurrent, "")
	}
	// Existing Forge trees: dynamic processor outputs cannot be proven identical to lock artifacts without execution.
	// Outputs retains the verified attributable launcher files and inputs for adoption.
	return v.result(
		CurrentnessInconclusive,
		"Forge installer processor outputs cannot be verified against locked artifact pins without execution",
	)
}

// verifyInputs verifies every locked input against its pinned digests. Forge
// inputs carry their declared contract policy; stage-only inputs are consumed
// by installer execution and verified only while staging.
func (v *treeVerifier) verifyInputs() {
	for _, input := range v.locked.Inputs {
		if err := document.SafeRelativePath(input.Path); err != nil {
			v.fail("unsafe locked input path %q: %v", input.Path, err)
			continue
		}
		cleanRel := filepath.ToSlash(filepath.Clean(input.Path))
		policy, role := PolicyManaged, RoleRuntime
		if v.locked.Bootstrapper == serverpkg.BootstrapMethodForgeInstaller {
			spec, declared := v.contract[cleanRel]
			if !declared {
				v.fail("locked input %s has no declared bootstrap contract", cleanRel)
				continue
			}
			if !v.staging && spec.stageOnly() {
				continue
			}
			policy, role = spec.Policy, spec.Role
		}
		out, ok := v.verifyPinned(cleanRel, input.Artifact.Hashes)
		if !ok {
			continue
		}
		out.Policy, out.Role = policy, role
		v.add(out)
	}
}

// verifyPinned verifies one required regular file against pinned digests.
func (v *treeVerifier) verifyPinned(rel string, hashes map[string]string) (Output, bool) {
	if err := checkPathSafety(v.serverDir, rel, true); err != nil {
		v.fail("%s safety: %v", rel, err)
		return Output{}, false
	}
	path := filepath.Join(v.serverDir, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		v.fail("missing or irregular %s", rel)
		return Output{}, false
	}
	digest, err := verifyArtifactHashes(path, hashes, v.cached[rel])
	if err != nil {
		v.fail("%s hash verification: %v", rel, err)
		return Output{}, false
	}
	return v.output(rel, digest, uint32(info.Mode().Perm()), ""), true
}

// collectRequiredOutput adds a required generated file to the verified outputs.
func (v *treeVerifier) collectRequiredOutput(rel string, policy OutputPolicy) {
	out, ok := v.verifyGeneratedOutput(rel, policy)
	if ok {
		v.add(out)
	}
}

// collectOptionalOutput adds a generated file only when it exists. Seeds such
// as README.txt are installed when missing but never required afterwards.
func (v *treeVerifier) collectOptionalOutput(rel string, policy OutputPolicy) {
	path := filepath.Join(v.serverDir, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if err := checkPathSafety(v.serverDir, rel, true); err != nil {
		v.fail("%v", err)
		return
	}
	digest := v.cached[rel]
	if digest == "" {
		digest, _, err = fileDigests(path, true, false)
	}
	if err != nil {
		v.fail("reading %s: %v", rel, err)
		return
	}
	v.add(v.output(rel, digest, uint32(info.Mode().Perm()), policy))
}

// verifyGeneratedOutput verifies one generated regular file. Its digest is not
// pinned by the lock, so the observed bytes become the accepted baseline.
func (v *treeVerifier) verifyGeneratedOutput(rel string, policy OutputPolicy) (Output, bool) {
	if err := checkPathSafety(v.serverDir, rel, true); err != nil {
		v.fail("%v", err)
		return Output{}, false
	}
	path := filepath.Join(v.serverDir, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		v.fail("missing or irregular %s", rel)
		return Output{}, false
	}
	digest := v.cached[rel]
	if digest == "" {
		digest, _, err = fileDigests(path, true, false)
	}
	if err != nil {
		v.fail("reading %s: %v", rel, err)
		return Output{}, false
	}
	return v.output(rel, digest, uint32(info.Mode().Perm()), policy), true
}

func (v *treeVerifier) output(rel, sha256 string, mode uint32, policy OutputPolicy) Output {
	return Output{
		Path:       workspacePath(v.locked, rel),
		ServerPath: rel,
		SHA256:     sha256,
		Mode:       mode,
		Policy:     policy,
		Role:       RoleRuntime,
	}
}

func (v *treeVerifier) add(out Output) {
	if v.seen[out.ServerPath] {
		return
	}
	v.seen[out.ServerPath] = true
	v.outputs = append(v.outputs, out)
}

func (v *treeVerifier) fail(format string, args ...any) {
	v.failed = true
	if v.reason == "" {
		v.reason = fmt.Sprintf(format, args...)
	}
}

func (v *treeVerifier) mismatch() CheckResult {
	return v.result(CurrentnessMismatch, v.reason)
}

func (v *treeVerifier) result(status CurrentnessStatus, reason string) CheckResult {
	slices.SortFunc(v.outputs, func(a, b Output) int {
		return strings.Compare(a.Path, b.Path)
	})
	return CheckResult{
		Current: status == CurrentnessCurrent,
		Status:  status,
		Reason:  reason,
		Outputs: v.outputs,
	}
}

// workspacePath converts a server-relative path to the workspace-relative
// path recorded by installation receipts.
func workspacePath(locked document.LockedServer, rel string) string {
	if locked.Directory == "" || locked.Directory == "." {
		return rel
	}
	return filepath.ToSlash(filepath.Clean(filepath.Join(locked.Directory, rel)))
}
