package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace/discovery"
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
	files *os.Root,
	root string,
	opts AdoptOptions,
	tracked map[string]bool,
	ledger *adoptionLedger,
) error {
	if opts.Lock.Server.Bootstrapper == "" && opts.Lock.Server.Product == "" {
		return nil
	}
	serverDir := filepath.Join(root, filepath.FromSlash(opts.Lock.Server.Directory))
	check, checkErr := bootstrap.Check(serverDir, opts.Lock.Server, coreArtifactPath(opts.Lock.Server))
	if checkErr != nil && opts.Runtime != nil {
		return fmt.Errorf("verify existing server runtime: %w", checkErr)
	}

	baselines := runtimeBaselines(opts)
	if check.Current && opts.Runtime != nil &&
		!observedRuntimeMatchesLocked(opts.Runtime, opts.Lock.Server) {
		check.Current = false
		check.Reason = "observed runtime does not match the locked server"
	}

	if check.Current {
		if err := claimBootstrapOutputs(
			files, root, check.Outputs, baselines, tracked, ledger,
		); err != nil {
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
		return nil
	}

	if opts.Logger != nil {
		opts.Logger.Debug(
			"recording attributable runtime ownership without bootstrap currentness",
			"reason", check.Reason,
		)
	}
	return adoptInconclusiveRuntime(files, root, opts, check.Outputs, tracked, ledger)
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

// observedRuntimeMatchesLocked reports whether the observed installation
// identity agrees with the resolved lock. Unknown observed versions do not
// contradict the lock.
func observedRuntimeMatchesLocked(
	installation *discovery.Installation,
	locked document.LockedServer,
) bool {
	if installation == nil {
		return false
	}
	identity := installation.Identity
	product := identity.Product()
	if product == server.ProductUnknown {
		product = server.Product(identity.Primary.Name.String())
	}
	if product != locked.Product {
		return false
	}
	return !observedVersionConflicts(identity.MinecraftVersion(), locked.Minecraft) &&
		!observedVersionConflicts(identity.Primary.Version, locked.CoreVersion)
}

// observedVersionConflicts reports a concrete observed version that differs
// from the locked version. Unknown observed metadata proves nothing.
func observedVersionConflicts(observed, locked versionpkg.Version) bool {
	if observed == "" || observed.IsInvalid() || observed.CanInfer() {
		return false
	}
	return observed != locked
}

// bootstrapImage returns the exact state a managed bootstrap output records.
func bootstrapImage(out bootstrap.Output) *workspacewrite.InstallImage {
	return &workspacewrite.InstallImage{
		SHA256: out.SHA256,
		Mode:   workspacewrite.EffectiveInstallMode(os.FileMode(out.Mode)),
	}
}

// adoptedImage returns the exact state captured for one discovered runtime
// file. Every capture uses the observed permission mode, including zero.
func adoptedImage(file AdoptedRuntimeFile) *workspacewrite.InstallImage {
	return &workspacewrite.InstallImage{
		SHA256: strings.ToLower(file.ExpectedSHA256),
		Mode:   file.ExpectedMode,
	}
}

// claimBootstrapOutputs rechecks every managed bootstrap output against the
// contract and its observed baseline, then records its ownership. The
// baseline map is nil when no runtime selection was observed.
func claimBootstrapOutputs(
	files *os.Root,
	root string,
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
		expected := bootstrapImage(out)
		if err := verifyOutputBaseline(files, cleanRel, expected); err != nil {
			return fmt.Errorf("%w: %v", errAdoptionOutputConflict, err)
		}
		if observed, ok := baselines[filepath.Clean(
			filepath.Join(root, filepath.FromSlash(cleanRel)),
		)]; ok && !workspacewrite.InstallImagesEqual(
			expected, adoptedImage(observed),
		) {
			return errAdoptionOutputConflict
		}
		if err := ledger.claim(
			cleanRel,
			receipt.OriginBootstrap,
			"",
			expected,
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
	files *os.Root,
	root string,
	opts AdoptOptions,
	outputs []bootstrap.Output,
	tracked map[string]bool,
	ledger *adoptionLedger,
) error {
	if err := claimBootstrapOutputs(
		files, root, outputs, nil, tracked, ledger,
	); err != nil {
		return err
	}

	if opts.Runtime == nil {
		return nil
	}
	baselines := runtimeBaselines(opts)
	specs := bootstrap.OutputSpecs(opts.Lock.Server)
	serverDir := cleanWorkspacePath(opts.Lock.Server.Directory)
	if serverDir == "." {
		serverDir = ""
	}
	for _, file := range opts.Runtime.Files {
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
		if !observed || baseline.ExpectedSHA256 == "" {
			return fmt.Errorf("runtime file %s has no observed baseline", cleanRel)
		}
		expected := adoptedImage(baseline)
		if err := verifyOutputBaseline(files, cleanRel, expected); err != nil {
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
			expected,
		); err != nil {
			return err
		}
	}
	return nil
}

// verifyOutputBaseline checks that an output still has the observed exact
// content and permission bits.
func verifyOutputBaseline(
	files *os.Root,
	rel string,
	expected *workspacewrite.InstallImage,
) error {
	current, err := workspacewrite.ObserveInstallImage(files, rel)
	switch {
	case err != nil:
		return fmt.Errorf("%s is not a regular file: %w", rel, err)
	case current == nil:
		return fmt.Errorf("inspect %s: file is absent", rel)
	}
	if !workspacewrite.InstallImagesEqual(current, expected) {
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
