package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
)

// errAdoptionOutputConflict reports that a verified runtime output no longer
// matches the bytes and mode the bootstrap contract observed.
var errAdoptionOutputConflict = errors.New(
	"verified server outputs changed or conflict with tracked files during adoption",
)

// adoptRuntime claims the runtime files the locked bootstrap contract proves.
// A current tree also records its bootstrap fingerprint; an inconclusive tree
// claims only outputs that attribution and discovery agree on.
func adoptRuntime(
	root string,
	opts AdoptOptions,
	tracked map[string]bool,
	ledger *adoptionLedger,
) error {
	if opts.Lock.Server.Bootstrapper == "" && opts.Lock.Server.Product == "" {
		return nil
	}
	serverDir := filepath.Join(root, filepath.FromSlash(opts.Lock.Server.Directory))
	check, checkErr := bootstrap.CheckExisting(serverDir, opts.Lock.Server)
	if checkErr != nil && opts.Runtime != nil {
		return fmt.Errorf("verify existing server runtime: %w", checkErr)
	}

	baselines := runtimeBaselines(opts)
	entryPoint := selectedEntryPoint(root, opts)
	if check.Current && entryPoint != "" &&
		!selectedRuntimeVerified(check.Outputs, entryPoint) {
		check.Current = false
		check.Reason = "selected runtime does not match verified server output"
	}

	if check.Current {
		if err := verifyCurrentRuntime(root, check.Outputs, baselines, entryPoint, tracked); err != nil {
			return err
		}
		fingerprint, err := receipt.BootstrapFingerprint(opts.Lock)
		if err != nil {
			return err
		}
		ledger.bootstrapFingerprint = fingerprint
		if opts.Logger != nil {
			opts.Logger.Debug(
				"existing server tree satisfies locked bootstrap contract",
				"method", opts.Lock.Server.Bootstrapper,
				"fingerprint", fingerprint,
				"outputs", len(check.Outputs),
			)
		}
		return claimBootstrapOutputs(check.Outputs, ledger)
	}

	if opts.Logger != nil {
		opts.Logger.Debug(
			"recording attributable runtime ownership without bootstrap currentness",
			"reason", check.Reason,
		)
	}
	return adoptInconclusiveRuntime(root, opts, check.Outputs, baselines, tracked, ledger)
}

// runtimeBaselines indexes the digest and mode captured for each observed
// runtime file.
func runtimeBaselines(opts AdoptOptions) map[string]AdoptedRuntimeFile {
	baselines := make(map[string]AdoptedRuntimeFile, len(opts.RuntimeFiles))
	for _, file := range opts.RuntimeFiles {
		baselines[filepath.Clean(file.Path)] = file
	}
	return baselines
}

// selectedEntryPoint returns the workspace-relative path of the runtime
// observation this initialization selected, or empty without a selection.
func selectedEntryPoint(root string, opts AdoptOptions) string {
	if opts.Runtime == nil || opts.Runtime.EntryPoint == nil {
		return ""
	}
	rel, err := filepath.Rel(root, opts.Runtime.EntryPoint.Path)
	if err != nil || !filepath.IsLocal(rel) {
		return ""
	}
	return cleanWorkspacePath(rel)
}

// selectedRuntimeVerified reports whether the verified tree contains the
// selected runtime. Another shared library inside the same installation files
// must not prove that the selected entry point satisfies the contract.
func selectedRuntimeVerified(outputs []bootstrap.Output, entryPoint string) bool {
	for _, out := range outputs {
		if out.Role == bootstrap.RoleRuntime &&
			cleanWorkspacePath(out.Path) == entryPoint {
			return true
		}
	}
	return false
}

// verifyCurrentRuntime rechecks every managed runtime output against its
// observed bytes, mode, and the baseline captured for the runtime selection.
func verifyCurrentRuntime(
	root string,
	outputs []bootstrap.Output,
	baselines map[string]AdoptedRuntimeFile,
	entryPoint string,
	tracked map[string]bool,
) error {
	for _, out := range outputs {
		if !isManagedRuntimeOutput(out) {
			continue
		}
		cleanRel := cleanWorkspacePath(out.Path)
		if !workspacePathSafe(root, cleanRel) {
			return fmt.Errorf(
				"verified bootstrap output %s escapes the workspace",
				out.Path,
			)
		}
		if tracked[strings.ToLower(cleanRel)] || installPathReserved(root, cleanRel) {
			return errAdoptionOutputConflict
		}
		if err := verifyOutputBaseline(root, cleanRel, out.SHA256, out.Mode); err != nil {
			return fmt.Errorf("%w: %v", errAdoptionOutputConflict, err)
		}
		if observed, ok := baselines[filepath.Clean(
			filepath.Join(root, filepath.FromSlash(cleanRel)),
		)]; ok {
			if !strings.EqualFold(observed.ExpectedSHA256, out.SHA256) ||
				(observed.ExpectedMode != 0 && observed.ExpectedMode != out.Mode) {
				return errAdoptionOutputConflict
			}
		}
	}
	if entryPoint != "" {
		if _, observed := baselines[filepath.Clean(
			filepath.Join(root, filepath.FromSlash(entryPoint)),
		)]; !observed {
			return fmt.Errorf(
				"selected entry point %s has no observed baseline",
				entryPoint,
			)
		}
	}
	return nil
}

func claimBootstrapOutputs(outputs []bootstrap.Output, ledger *adoptionLedger) error {
	for _, out := range outputs {
		if !isManagedRuntimeOutput(out) {
			continue
		}
		cleanRel := cleanWorkspacePath(out.Path)
		if err := ledger.claim(
			cleanRel,
			receipt.OriginBootstrap,
			"",
			out.SHA256,
			out.Mode,
		); err != nil {
			return err
		}
	}
	return nil
}

// adoptInconclusiveRuntime records ownership of files the bootstrap contract
// cannot prove current. It refuses outputs that conflict with workspace state
// and claims discovery runtime files only inside declared managed scopes.
func adoptInconclusiveRuntime(
	root string,
	opts AdoptOptions,
	outputs []bootstrap.Output,
	baselines map[string]AdoptedRuntimeFile,
	tracked map[string]bool,
	ledger *adoptionLedger,
) error {
	for _, out := range outputs {
		if !isManagedRuntimeOutput(out) {
			continue
		}
		cleanRel := cleanWorkspacePath(out.Path)
		if !workspacePathSafe(root, cleanRel) ||
			tracked[strings.ToLower(cleanRel)] ||
			installPathReserved(root, cleanRel) {
			return fmt.Errorf(
				"verified bootstrap output %s conflicts with workspace state",
				out.Path,
			)
		}
		if err := verifyOutputBaseline(root, cleanRel, out.SHA256, out.Mode); err != nil {
			return fmt.Errorf(
				"verified bootstrap output %s changed during adoption: %w",
				out.Path,
				err,
			)
		}
		if err := ledger.claim(
			cleanRel,
			receipt.OriginBootstrap,
			"",
			out.SHA256,
			out.Mode,
		); err != nil {
			return err
		}
	}

	if opts.Runtime == nil || opts.Runtime.Installation == nil {
		return nil
	}
	specs := bootstrap.OutputSpecs(opts.Lock.Server)
	serverDir := cleanWorkspacePath(opts.Lock.Server.Directory)
	if serverDir == "." {
		serverDir = ""
	}
	for _, file := range opts.Runtime.Installation.Files {
		cleanRel, ok := workspaceRelative(root, file.Path)
		if !ok {
			return fmt.Errorf("runtime file %s escapes the workspace", file.Path)
		}
		if !workspacePathSafe(root, cleanRel) ||
			tracked[strings.ToLower(cleanRel)] ||
			installPathReserved(root, cleanRel) {
			continue
		}
		serverRel, within := serverRelative(serverDir, cleanRel)
		if !within || !managedRuntimeScope(specs, serverRel) {
			continue
		}
		baseline, observed := baselines[filepath.Clean(file.Path)]
		if !observed {
			if info, err := os.Lstat(file.Path); err == nil &&
				info.Mode()&os.ModeSymlink != 0 {
				// Aliases stay unowned.
				continue
			}
		}
		if !observed || baseline.ExpectedSHA256 == "" || baseline.ExpectedMode == 0 {
			return fmt.Errorf("runtime file %s has no observed baseline", cleanRel)
		}
		if err := verifyOutputBaseline(
			root,
			cleanRel,
			baseline.ExpectedSHA256,
			baseline.ExpectedMode,
		); err != nil {
			return fmt.Errorf(
				"runtime file %s changed between discovery and adoption publication: %w",
				cleanRel,
				err,
			)
		}
		if err := ledger.claim(
			cleanRel,
			receipt.OriginBootstrap,
			"",
			baseline.ExpectedSHA256,
			baseline.ExpectedMode,
		); err != nil {
			return err
		}
	}
	return nil
}

// verifyOutputBaseline checks that an output still has the observed digest and
// permission bits.
func verifyOutputBaseline(root, rel, expectedSHA256 string, expectedMode uint32) error {
	target := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(target)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", rel, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", rel)
	}
	hashes, err := DigestFile(target, nil)
	if err != nil {
		return fmt.Errorf("digest %s: %w", rel, err)
	}
	if !strings.EqualFold(hashes["sha256"], expectedSHA256) ||
		uint32(info.Mode().Perm()) != expectedMode {
		return fmt.Errorf("%s changed", rel)
	}
	return nil
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

// serverRelative rebases a workspace-relative path onto the server directory.
func serverRelative(serverDir, rel string) (string, bool) {
	if serverDir == "" {
		return rel, true
	}
	if rel == serverDir {
		return "", true
	}
	if !strings.HasPrefix(rel, serverDir+"/") {
		return "", false
	}
	return strings.TrimPrefix(rel, serverDir+"/"), true
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
