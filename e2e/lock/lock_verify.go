package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"gopkg.in/yaml.v3"
)

// Wire types keep assertions independent of Lucy's codecs and fingerprint code.
type manifestIntent struct {
	Server struct {
		Minecraft string `yaml:"minecraft"`
		Directory string `yaml:"directory"`
		Core      struct {
			Distribution string `yaml:"distribution"`
			Version      string `yaml:"version"`
		} `yaml:"core"`
		Packages map[string]map[string]string `yaml:"packages"`
	} `yaml:"server"`
}

type lockedArtifact struct {
	Provider  string            `yaml:"provider"`
	ProjectID string            `yaml:"project_id"`
	ReleaseID string            `yaml:"release_id"`
	FileID    string            `yaml:"file_id"`
	Version   string            `yaml:"version"`
	Filename  string            `yaml:"filename"`
	URL       string            `yaml:"url"`
	Hashes    map[string]string `yaml:"hashes"`
	Modules   []lockedModule    `yaml:"modules"`
}

type lockedModule struct {
	ID           string   `yaml:"id"`
	Version      string   `yaml:"version"`
	Member       string   `yaml:"member"`
	Provides     []string `yaml:"provides"`
	Dependencies []struct {
		ID         string `yaml:"id"`
		Kind       string `yaml:"kind"`
		Constraint string `yaml:"constraint"`
	} `yaml:"dependencies"`
}

type lockedPackage struct {
	Runtime        string `yaml:"runtime"`
	Loader         string `yaml:"loader"`
	lockedArtifact `yaml:",inline"`
	Requirements   []struct {
		Reference string `yaml:"reference"`
		Selector  string `yaml:"selector"`
		Enabled   bool   `yaml:"enabled"`
	} `yaml:"requirements"`
}

type lockDocument struct {
	ManifestFingerprint string `yaml:"manifest_fingerprint"`
	Server              struct {
		Minecraft      string `yaml:"minecraft"`
		Directory      string `yaml:"directory"`
		Distribution   string `yaml:"distribution"`
		CoreVersion    string `yaml:"core_version"`
		lockedArtifact `yaml:",inline"`
		Inputs         []struct {
			Path           string `yaml:"path"`
			lockedArtifact `yaml:",inline"`
		} `yaml:"inputs"`
	} `yaml:"server"`
	Packages []lockedPackage `yaml:"packages"`
}

type lockSnapshot struct {
	document lockDocument
	semantic any
}

func readLock(root string) (lockSnapshot, error) {
	data, err := os.ReadFile(filepath.Join(root, "lucy-lock.yaml"))
	if err != nil {
		return lockSnapshot{}, fmt.Errorf("read generated lock: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return lockSnapshot{}, fmt.Errorf("decode generated lock: %w", err)
	}
	var snapshot lockSnapshot
	if err := node.Decode(&snapshot.document); err != nil {
		return lockSnapshot{}, err
	}
	if err := node.Decode(&snapshot.semantic); err != nil {
		return lockSnapshot{}, err
	}
	return snapshot, nil
}

func (want lockSnapshot) unchanged(root string) error {
	got, err := readLock(root)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(want.semantic, got.semantic) {
		return fmt.Errorf("exact replay changed decoded lock contents")
	}
	return nil
}

func (scenario lockScenario) verify(root, fixture string, declared manifestIntent, locked lockDocument) error {
	server := locked.Server
	if server.Minecraft != declared.Server.Minecraft || server.Distribution != declared.Server.Core.Distribution {
		return fmt.Errorf("locked Minecraft/distribution = %s/%s, want %s/%s",
			server.Minecraft, server.Distribution, declared.Server.Minecraft, declared.Server.Core.Distribution)
	}
	if server.CoreVersion != declared.Server.Core.Version || server.Directory != declared.Server.Directory {
		return fmt.Errorf("locked core/directory = %s/%s, want %s/%s",
			server.CoreVersion, server.Directory, declared.Server.Core.Version, declared.Server.Directory)
	}
	live := filepath.Join(root, server.Directory)
	if err := verifyOracleFile(filepath.Join(fixture, "server.jar"), filepath.Join(live, "server.jar")); err != nil {
		return fmt.Errorf("Minecraft artifact: %w", err)
	}
	corePath := filepath.Join(live, "server.jar")
	if server.Distribution == "vanilla" {
		if server.Provider != "mojang" || server.ProjectID != "minecraft" || server.Version != server.Minecraft {
			return fmt.Errorf("vanilla lock did not retain Mojang's exact server coordinates")
		}
	} else {
		if server.Provider != "fabric" || server.ProjectID != "fabric-loader" || server.Version != server.CoreVersion {
			return fmt.Errorf("Fabric lock did not retain the pinned loader coordinates")
		}
		corePath = filepath.Join(live, "libraries", "net", "fabricmc", "fabric-loader", server.CoreVersion, server.Filename)
	}
	if err := verifyLockedFile(corePath, server.lockedArtifact); err != nil {
		return err
	}
	minecraftInput := server.Distribution == "vanilla"
	for _, input := range server.Inputs {
		if input.Path == "server.jar" {
			minecraftInput = input.Provider == "mojang" && input.Version == server.Minecraft
		}
		if err := verifyLockedFile(filepath.Join(live, filepath.FromSlash(input.Path)), input.lockedArtifact); err != nil {
			return fmt.Errorf("bootstrap input %s: %w", input.Path, err)
		}
	}
	if !minecraftInput {
		return fmt.Errorf("Fabric bootstrap omitted its pinned Minecraft input")
	}
	seen := map[string]bool{}
	for _, pkg := range locked.Packages {
		index := slices.IndexFunc(scenario.packages, func(want packageExpectation) bool {
			return pkg.ProjectID == want.projectID
		})
		if index < 0 || seen[pkg.ProjectID] {
			return fmt.Errorf("unexpected or duplicated locked project %q", pkg.ProjectID)
		}
		seen[pkg.ProjectID] = true
		want := scenario.packages[index]
		if pkg.Provider != "modrinth" || pkg.Runtime != "server" || pkg.Loader != "fabric" {
			return fmt.Errorf("project %s has incorrect provider/runtime/loader coordinates", pkg.ProjectID)
		}
		if pkg.ReleaseID != want.releaseID || pkg.FileID != want.fileID || pkg.Filename != want.filename {
			return fmt.Errorf("project %s selected release/file %s/%s (%s), want %s/%s (%s)",
				pkg.ProjectID, pkg.ReleaseID, pkg.FileID, pkg.Filename, want.releaseID, want.fileID, want.filename)
		}
		selector := declared.Server.Packages["fabric"][want.reference]
		if pkg.Version != selector || !slices.ContainsFunc(pkg.Requirements, func(req struct {
			Reference string `yaml:"reference"`
			Selector  string `yaml:"selector"`
			Enabled   bool   `yaml:"enabled"`
		},
		) bool {
			return req.Reference == want.reference && req.Selector == selector && req.Enabled
		}) {
			return fmt.Errorf("project %s lost its exact manifest selector %q", pkg.ProjectID, selector)
		}
		path := filepath.Join(live, "mods", pkg.Filename)
		if err := verifyOracleFile(filepath.Join(fixture, "mods", pkg.Filename), path); err != nil {
			return err
		}
		if err := verifyLockedFile(path, pkg.lockedArtifact); err != nil {
			return err
		}
		if err := verifyNativeModules(pkg); err != nil {
			return err
		}
	}
	if len(seen) != len(scenario.packages) {
		return fmt.Errorf("lock omitted a declared package: selected %v", seen)
	}
	return nil
}

func verifyNativeModules(pkg lockedPackage) error {
	id, version, member := "fabric-api-base", "0.4.54+b47eab6b04", "META-INF/jars/fabric-api-base-0.4.54+b47eab6b04.jar"
	if pkg.ProjectID == "Si383TIH" {
		id, version, member = "lingua_bib", "1.21.4-1.0.3", "META-INF/jars/lingua_bib-1.21.4-1.0.3-fabric.jar"
		flan := slices.IndexFunc(pkg.Modules, func(module lockedModule) bool { return module.ID == "flan" })
		if flan < 0 || pkg.Modules[flan].Version != "1.21.4-1.11.7.b" {
			return fmt.Errorf("Flan catalogue version replaced its native module identity/version")
		}
		found := false
		for _, dependency := range pkg.Modules[flan].Dependencies {
			found = found || (dependency.ID == "fabric" && dependency.Kind == "required" && dependency.Constraint == ">=0.119.2+1.21.4")
		}
		if !found {
			return fmt.Errorf("Flan lost its required native dependency on the Fabric API alias")
		}
	} else {
		api := slices.IndexFunc(pkg.Modules, func(module lockedModule) bool { return module.ID == "fabric-api" })
		if api < 0 || pkg.Modules[api].Version != pkg.Version || !slices.Contains(pkg.Modules[api].Provides, "fabric") {
			return fmt.Errorf("Fabric API lost its native identity/version or provided fabric alias")
		}
	}
	if !slices.ContainsFunc(pkg.Modules, func(module lockedModule) bool {
		return module.ID == id && module.Version == version && module.Member == member
	}) {
		return fmt.Errorf("project %s lost embedded module %s (%s)", pkg.ProjectID, id, member)
	}
	return nil
}

func verifyLockedFile(path string, artifact lockedArtifact) error {
	hash, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if artifact.Hashes["sha256"] != hash {
		return fmt.Errorf("locked digest for %s does not match its installed bytes", path)
	}
	return nil
}

func verifyOracleFile(fixture, live string) error {
	want, err := fileSHA256(fixture)
	if err != nil {
		return fmt.Errorf("fixture unavailable (%s): %w", fixture, err)
	}
	got, err := fileSHA256(live)
	if err != nil {
		return err
	}
	if want != got {
		return fmt.Errorf("installed %s differs from the pinned envgen artifact", live)
	}
	return nil
}

type fileDigest struct {
	SHA256 string
	Mode   fs.FileMode
}

func fileDigests(root string) (map[string]fileDigest, error) {
	files := map[string]fileDigest{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) && path == root {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".lucy" {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		hash, err := fileSHA256(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = fileDigest{SHA256: hash, Mode: info.Mode().Perm()}
		return nil
	})
	return files, err
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
