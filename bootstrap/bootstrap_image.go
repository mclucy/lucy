package bootstrap

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
)

// StagedImage classifies and digests every file a method produced under the
// server root of stageRoot. The result is the runtime image of the recipe:
// each file with its policy and role from the method's declared outputs,
// sorted by path. Stage-only material is listed with its policy so the
// commit discards it.
//
// The method is the only writer of the stage, so the image needs no proof
// beyond its declaration. A file outside every declared output or a missing
// exact output is a method defect and fails the build.
func StagedImage(stageRoot string, locked document.LockedServer) ([]Output, error) {
	exact, scopes, err := validateContract(locked)
	if err != nil {
		return nil, fmt.Errorf("invalid bootstrap output contract: %w", err)
	}
	optional := optionalOutputs(locked)
	serverRoot, err := stagedServerRoot(stageRoot, locked.Directory)
	if err != nil {
		return nil, err
	}

	outputs := make([]Output, 0, len(exact))
	produced := make(map[string]bool, len(exact))
	err = filepath.WalkDir(serverRoot, func(p string, entry fs.DirEntry, walkErr error) error {
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
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("bootstrap method %s wrote symlink %s", locked.Bootstrap, rel)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("bootstrap method %s wrote non-regular file %s", locked.Bootstrap, rel)
		}
		spec, declared := exact[rel]
		if !declared {
			spec, declared = optional[rel]
		}
		if !declared {
			spec, declared = scopeOutput(rel, scopes)
		}
		if !declared {
			return fmt.Errorf("bootstrap method %s wrote undeclared file %s", locked.Bootstrap, rel)
		}
		digest, err := fileSHA256(p)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		produced[rel] = true
		outputs = append(outputs, Output{
			Path:       filepath.ToSlash(filepath.Join(locked.Directory, rel)),
			ServerPath: rel,
			SHA256:     digest,
			Mode:       uint32(info.Mode().Perm()),
			Policy:     spec.Policy,
			Role:       spec.Role,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, rel := range slices.Sorted(maps.Keys(exact)) {
		spec := exact[rel]
		if spec.Policy != PolicyManaged || produced[rel] {
			continue
		}
		return nil, fmt.Errorf("bootstrap method %s did not produce %s", locked.Bootstrap, rel)
	}
	slices.SortFunc(outputs, func(a, b Output) int {
		return strings.Compare(a.Path, b.Path)
	})
	return outputs, nil
}

// optionalOutputs declares the server-root files a method writes outside its
// exact paths and scopes. A shim-era Forge installer extracts its profile
// launch artifact at the server root. Later installer generations write no
// root shim, so the path is classified when present and never required.
func optionalOutputs(locked document.LockedServer) map[string]OutputSpec {
	shim := forgeRootShimPath(locked)
	if shim == "" {
		return nil
	}
	return map[string]OutputSpec{
		shim: {
			Path:   shim,
			Policy: PolicyManaged,
			Role:   RoleRuntime,
			Origin: OriginGenerated,
		},
	}
}

// scopeOutput returns the declaration of the longest scope that bounds rel.
func scopeOutput(rel string, scopes []OutputSpec) (OutputSpec, bool) {
	var matched OutputSpec
	found := false
	for _, scope := range scopes {
		if !strings.HasPrefix(rel, scope.Scope) {
			continue
		}
		if !found || len(scope.Scope) > len(matched.Scope) {
			matched, found = scope, true
		}
	}
	return matched, found
}

// stagedServerRoot rejects a symlink or non-directory in the staged server path.
func stagedServerRoot(stage, directory string) (string, error) {
	relative := filepath.FromSlash(cmp.Or(directory, "."))
	if err := document.SafeRelativePath(relative); err != nil {
		return "", err
	}
	relative = filepath.Clean(relative)
	root := filepath.Clean(stage)
	for {
		info, err := os.Lstat(root)
		if err != nil {
			return "", fmt.Errorf("inspect staged server directory: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("staged server path %s is not a directory", root)
		}
		if relative == "." || relative == "" {
			return root, nil
		}
		part, rest, _ := strings.Cut(relative, string(filepath.Separator))
		root = filepath.Join(root, part)
		relative = rest
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
