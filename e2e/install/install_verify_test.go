//go:build e2e

package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Wire types keep assertions independent of Lucy's codecs and fingerprint code.
type manifestIntent struct {
	Server struct {
		Minecraft string `yaml:"minecraft"`
		Directory string `yaml:"directory"`
		Core      struct {
			Product        string `yaml:"product"`
			ProductVersion string `yaml:"product_version"`
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
	ID             string   `yaml:"id"`
	Version        string   `yaml:"version"`
	Ecosystem      string   `yaml:"ecosystem"`
	NestedPath     string   `yaml:"nested_path"`
	DescriptorPath string   `yaml:"descriptor_path"`
	Provides       []string `yaml:"provides"`
	Dependencies   []struct {
		ID         string `yaml:"id"`
		Kind       string `yaml:"kind"`
		Constraint string `yaml:"constraint"`
	} `yaml:"dependencies"`
}

type lockedRequirement struct {
	Reference string `yaml:"reference"`
	Selector  string `yaml:"selector"`
	Enabled   bool   `yaml:"enabled"`
}

type lockedPackage struct {
	Environment    string `yaml:"environment"`
	Ecosystem      string `yaml:"ecosystem"`
	lockedArtifact `yaml:",inline"`
	Requirements   []lockedRequirement `yaml:"requirements"`
}

type lockedInput struct {
	Path           string `yaml:"path"`
	lockedArtifact `yaml:",inline"`
}

type lockDocument struct {
	Version             int    `yaml:"lock_version"`
	ManifestFingerprint string `yaml:"manifest_fingerprint"`
	Server              struct {
		Minecraft      string `yaml:"minecraft"`
		Directory      string `yaml:"directory"`
		Product        string `yaml:"product"`
		CoreVersion    string `yaml:"core_version"`
		Bootstrapper   string `yaml:"bootstrapper"`
		lockedArtifact `yaml:",inline"`
		Inputs         []lockedInput `yaml:"inputs"`
	} `yaml:"server"`
	Packages []lockedPackage `yaml:"packages"`
}

func readLock(root string) (lockDocument, error) {
	data, err := os.ReadFile(filepath.Join(root, "lucy-lock.yaml"))
	if err != nil {
		return lockDocument{}, fmt.Errorf("read generated lock: %w", err)
	}
	var document lockDocument
	if err := yaml.Unmarshal(data, &document); err != nil {
		return lockDocument{}, fmt.Errorf("decode generated lock: %w", err)
	}
	return document, nil
}

func (scenario installScenario) verify(root, oracle string, declared manifestIntent, locked lockDocument) error {
	server := locked.Server
	if server.Minecraft != scenario.minecraft || server.Minecraft != declared.Server.Minecraft {
		return fmt.Errorf("lock Minecraft version does not match the pinned manifest")
	}
	if server.Product != scenario.product || server.Product != declared.Server.Core.Product {
		return fmt.Errorf("lock core product does not match the pinned manifest")
	}
	if server.CoreVersion != scenario.coreVersion || server.CoreVersion != declared.Server.Core.ProductVersion {
		return fmt.Errorf("lock core version does not match the pinned manifest")
	}
	if server.Directory != declared.Server.Directory {
		return fmt.Errorf("lock server directory does not match the pinned manifest")
	}
	if server.Provider != scenario.provider || server.ProjectID != scenario.projectID {
		return fmt.Errorf("lock has incorrect %s provider or project identity", scenario.product)
	}
	if server.Version != scenario.coreVersion || server.Filename != scenario.filename {
		return fmt.Errorf("lock has incorrect %s core version or filename", scenario.product)
	}
	if server.Bootstrapper != scenario.bootstrap {
		return fmt.Errorf("lock has incorrect %s bootstrap profile", scenario.product)
	}

	live := filepath.Join(root, server.Directory)
	if scenario.coreOutput != "" {
		path := filepath.Join(live, filepath.FromSlash(scenario.coreOutput))
		if err := verifyLockedFile(path, server.lockedArtifact); err != nil {
			return err
		}
	}
	if scenario.coreFixture != "" {
		actual, err := fileSHA256(filepath.Join(oracle, scenario.coreFixture))
		if err != nil {
			return fmt.Errorf("core fixture %s: %w", scenario.coreFixture, err)
		}
		if actual != server.Hashes["sha256"] {
			return fmt.Errorf("selected %s core artifact differs from its pinned sandbox artifact", scenario.product)
		}
	}
	for _, artifact := range scenario.runtimeArtifacts {
		if err := verifyOracleFile(filepath.Join(oracle, artifact.sandboxFile), filepath.Join(live, artifact.runtimeFile)); err != nil {
			return fmt.Errorf("runtime artifact %s: %w", artifact.runtimeFile, err)
		}
	}
	switch {
	case scenario.minecraftFixture != "":
		if err := verifyOracleFile(filepath.Join(oracle, scenario.minecraftFixture), filepath.Join(live, "server.jar")); err != nil {
			return fmt.Errorf("minecraft server input: %w", err)
		}
	case scenario.minecraftDigest != "":
		if err := verifyPinnedDigest(filepath.Join(live, "server.jar"), scenario.minecraftDigest); err != nil {
			return fmt.Errorf("minecraft server input: %w", err)
		}
	}
	if err := verifyBootstrapInputs(live, server.Inputs, scenario.requiredInputs); err != nil {
		return err
	}
	if err := verifyRuntimeFiles(live, scenario); err != nil {
		return err
	}
	if err := verifyPackages(live, oracle, declared, scenario.packages, locked.Packages, scenario.product); err != nil {
		return err
	}
	return nil
}

func verifyBootstrapInputs(root string, inputs []lockedInput, required []string) error {
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		seen[input.Path] = true
		path := filepath.Join(root, filepath.FromSlash(input.Path))
		if err := verifyLockedFile(path, input.lockedArtifact); err != nil {
			return fmt.Errorf("bootstrap input %s: %w", input.Path, err)
		}
	}
	for _, path := range required {
		if !seen[path] {
			return fmt.Errorf("lock omitted required bootstrap input %s", path)
		}
	}
	return nil
}

func verifyRuntimeFiles(root string, scenario installScenario) error {
	for _, relative := range scenario.requiredFiles {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return fmt.Errorf("required runtime file %s: %w", relative, err)
		}
		if relative == "run.sh" && info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("generated launcher %s is not executable", relative)
		}
	}
	if scenario.launchArguments != "" {
		path := filepath.Join(root, filepath.FromSlash(scenario.launchArguments))
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("generated launch arguments %s: %w", scenario.launchArguments, err)
		}
		script, err := os.ReadFile(filepath.Join(root, "run.sh"))
		if err != nil {
			return err
		}
		if !strings.Contains(string(script), "@"+scenario.launchArguments) {
			return fmt.Errorf("generated launcher does not reference %s", scenario.launchArguments)
		}
	}
	if scenario.product == "fabric" {
		script, err := os.ReadFile(filepath.Join(root, "run.sh"))
		if err != nil {
			return err
		}
		if !strings.Contains(string(script), "net.fabricmc.loader.impl.launch.server.FabricServerLauncher") {
			return fmt.Errorf("generated launcher does not start the Fabric server")
		}
	}
	return nil
}

func verifyPackages(root, oracle string, declared manifestIntent, expected []packageExpectation, packages []lockedPackage, loader string) error {
	seen := make(map[string]bool, len(packages))
	for _, pkg := range packages {
		index := slices.IndexFunc(expected, func(want packageExpectation) bool {
			return pkg.ProjectID == want.ProjectID
		})
		if index < 0 || seen[pkg.ProjectID] {
			return fmt.Errorf("unexpected or duplicated locked project %q", pkg.ProjectID)
		}
		seen[pkg.ProjectID] = true
		want := expected[index]
		if pkg.Provider != "modrinth" || pkg.Environment != "server" {
			return fmt.Errorf("project %s has incorrect provider or environment", pkg.ProjectID)
		}
		if pkg.Ecosystem != loader {
			return fmt.Errorf("project %s has incorrect ecosystem", pkg.ProjectID)
		}
		switch selector, declaredInManifest := declared.Server.Packages[loader][want.Reference]; {
		case declaredInManifest:
			if pkg.ReleaseID != want.ReleaseID || pkg.FileID != want.FileID {
				return fmt.Errorf("project %s has incorrect release or file identity", pkg.ProjectID)
			}
			if pkg.Filename != want.Filename {
				return fmt.Errorf("project %s has incorrect filename", pkg.ProjectID)
			}
			if pkg.Version != selector || !slices.ContainsFunc(pkg.Requirements, func(req lockedRequirement) bool {
				return req.Reference == want.Reference && req.Selector == selector && req.Enabled
			}) {
				return fmt.Errorf("project %s lost its exact manifest selector %q", pkg.ProjectID, selector)
			}
		case want.Dependency:
			// A dependency advances whenever upstream publishes a version its
			// requirer's constraint accepts, so the resolved release is not a
			// stable fact about this workspace. Only its presence without a
			// manifest requirement and its installed bytes are.
			if len(pkg.Requirements) != 0 {
				return fmt.Errorf("dependency %s recorded a manifest requirement", pkg.ProjectID)
			}
		default:
			return fmt.Errorf("project %s is neither declared in the manifest nor marked as a dependency", pkg.ProjectID)
		}
		path := filepath.Join(root, "mods", pkg.Filename)
		switch {
		case want.SHA256 != "":
			if err := verifyPinnedDigest(path, want.SHA256); err != nil {
				return fmt.Errorf("project %s: %w", pkg.ProjectID, err)
			}
		case !want.Dependency && oracle != "":
			if err := verifyOracleFile(filepath.Join(oracle, "mods", pkg.Filename), path); err != nil {
				return err
			}
		}
		if err := verifyLockedFile(path, pkg.lockedArtifact); err != nil {
			return err
		}
		if err := verifyNativeModules(pkg); err != nil {
			return err
		}
	}
	if len(seen) != len(expected) {
		return fmt.Errorf("lock omitted a declared package: selected %v", seen)
	}
	return nil
}

func verifyNativeModules(pkg lockedPackage) error {
	switch pkg.ProjectID {
	case "P7dR8mSH":
		api := slices.IndexFunc(pkg.Modules, func(module lockedModule) bool { return module.ID == "fabric-api" })
		if api < 0 {
			return fmt.Errorf("fabric api lock omitted its native module")
		}
		if pkg.Modules[api].Version != pkg.Version || !slices.Contains(pkg.Modules[api].Provides, "fabric") {
			return fmt.Errorf("fabric api lock lost its native identity or provided alias")
		}
		if !slices.ContainsFunc(pkg.Modules, func(module lockedModule) bool {
			return module.ID == "fabric-api-base" && strings.HasPrefix(module.NestedPath, "META-INF/jars/fabric-api-base-")
		}) {
			return fmt.Errorf("fabric api lock lost its embedded base module")
		}
		return nil
	case "Si383TIH":
		flan := slices.IndexFunc(pkg.Modules, func(module lockedModule) bool { return module.ID == "flan" })
		if flan < 0 || pkg.Modules[flan].Version != "1.21.4-1.11.7.b" {
			return fmt.Errorf("flan lock lost its native module version")
		}
		found := false
		for _, dependency := range pkg.Modules[flan].Dependencies {
			if dependency.ID != "fabric" || dependency.Kind != "required" {
				continue
			}
			if dependency.Constraint == ">=0.119.2+1.21.4" {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("flan lock lost its required dependency on the Fabric API alias")
		}
		return verifyEmbeddedModule(pkg, "lingua_bib", "1.21.4-1.0.3", "META-INF/jars/lingua_bib-1.21.4-1.0.3-fabric.jar")
	default:
		return nil
	}
}

func verifyEmbeddedModule(pkg lockedPackage, id, version, member string) error {
	if !slices.ContainsFunc(pkg.Modules, func(module lockedModule) bool {
		matchesIDAndVersion := module.ID == id && module.Version == version
		return matchesIDAndVersion && module.NestedPath == member
	}) {
		return fmt.Errorf("project %s lock lost embedded module %s", pkg.ProjectID, id)
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

// verifyOracleFile compares an installed file against its counterpart in the
// generated sandbox, which the sandbox manifest pins by digest.
func verifyOracleFile(sandboxFile, installed string) error {
	want, err := fileSHA256(sandboxFile)
	if err != nil {
		return fmt.Errorf("fixture unavailable (%s): %w", sandboxFile, err)
	}
	return verifyPinnedDigest(installed, want)
}

// verifyPinnedDigest checks an installed file against a digest pinned in the
// scenario table, so fixtures that are not real servers need neither a
// generated directory nor a binary committed to the repository.
func verifyPinnedDigest(path, want string) error {
	got, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("installed %s has digest %s, want pinned %s", path, got, want)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", errors.Join(err, file.Close())
	}
	return hex.EncodeToString(hash.Sum(nil)), file.Close()
}
