package install

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
)

type AmbientDependencies struct {
	entries map[string]types.VersionedPackageRef
	aliases map[string]string
}

// Ambient dependencies are dependency ids already contributed by the server
// environment before Lucy resolves requested packages. They are separate from
// topology identity packages: topology describes the server shape, while this
// set describes what the mod loader can satisfy during dependency resolution.
func buildAmbientDependencies(
	ctx context.Context,
	ws workspace.Workspace,
) (AmbientDependencies, error) {
	ambient := AmbientDependencies{
		entries: make(map[string]types.VersionedPackageRef),
		aliases: make(map[string]string),
	}
	if ws.Environments.Mcdr != nil {
		ambient.Add(
			types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Eco:  types.EcoMcdr,
					Name: "mcdreforged",
				},
				Version: types.BareVersion(strings.TrimSpace(ws.Environments.Mcdr.Version.String())),
			},
		)
	}

	if ws.Runtime == nil || ws.Topology == nil || !ws.Topology.HasCapability(types.CapabilityFabricMods) {
		return ambient, nil
	}

	loaderVersion := ws.DerivedLoaderVersion()
	if loaderVersion != "" && loaderVersion != types.VersionUnknown.String() {
		ambient.Add(
			types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Eco: types.EcoFabric, Name: "fabricloader",
				},
				Version: types.BareVersion(loaderVersion),
			},
		)
	}
	ambient.Add(
		types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Eco: types.EcoFabric, Name: "minecraft",
			},
			Version: ws.Runtime.GameVersion,
		},
	)
	ambient.Add(
		types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Eco: types.EcoFabric, Name: "fabric",
			},
			Version: types.VersionAny,
		},
	)
	ambient.Add(
		types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Eco: types.EcoFabric, Name: "fabric-api",
			},
			Version: types.VersionAny,
		},
	)

	if javaVersion, ok := currentJavaSpecVersion(ctx); ok {
		ambient.Add(
			types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Eco: types.EcoFabric, Name: "java",
				},
				Version: javaVersion,
			},
		)
	}

	// Fabric Loader can also contribute real Fabric mods from its own artifact.
	// Loader 0.15+ uses this path for MixinExtras, and future nested loader mods
	// should be discovered from metadata rather than added as a name list.
	loaderPath := fabricLoaderArtifactPath(ws.Root, loaderVersion)
	if loaderPath == "" {
		return ambient, nil
	}
	if err := addFabricArtifactAmbientDependencies(
		&ambient,
		loaderPath,
	); err != nil {
		return AmbientDependencies{}, err
	}

	return ambient, nil
}

func (a *AmbientDependencies) Add(id types.VersionedPackageRef) {
	if id.Eco == "" || id.Name == "" {
		return
	}
	if a.entries == nil {
		a.entries = make(map[string]types.VersionedPackageRef)
	}
	a.entries[id.StringBase()] = id
}

func (a *AmbientDependencies) AddAlias(
	alias types.PackageRef,
	target types.VersionedPackageRef,
) {
	if alias.Eco == "" || alias.Name == "" || target.Eco == "" || target.Name == "" {
		return
	}
	if a.aliases == nil {
		a.aliases = make(map[string]string)
	}
	a.aliases[alias.StringBase()] = target.StringBase()
}

func (a AmbientDependencies) Mark(dep types.Dependency) types.Dependency {
	dep.Type = types.NormalizeDependencyType(dep.Type)
	if a.Satisfies(dep) {
		dep.Type = types.Ambient
	}
	return dep
}

func (a AmbientDependencies) Satisfies(dep types.Dependency) bool {
	key := dep.Id.StringBase()
	if targetKey, ok := a.aliases[key]; ok {
		key = targetKey
	}
	id, ok := a.entries[key]
	if !ok {
		return false
	}
	if dep.Constraint == nil {
		return true
	}
	if id.Version == types.VersionAny {
		return true
	}
	parsed, err := version.Parse(id.Version, types.Semver)
	if err != nil {
		return false
	}
	return dep.Satisfy(
		types.VersionedPackageRef{
			PackageRef: dep.Id.PackageRef, Version: id.Version,
		},
		parsed,
	)
}

func fabricLoaderArtifactPath(root, loaderVersion string) string {
	if root == "" || loaderVersion == "" || loaderVersion == types.VersionUnknown.String() {
		return ""
	}
	path := filepath.Join(
		root,
		"libraries",
		"net",
		"fabricmc",
		"fabric-loader",
		loaderVersion,
		fmt.Sprintf("fabric-loader-%s.jar", loaderVersion),
	)
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

func addFabricArtifactAmbientDependencies(
	ambient *AmbientDependencies,
	filePath string,
) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open fabric runtime artifact: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat fabric runtime artifact: %w", err)
	}
	zipRdr, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return fmt.Errorf("read fabric runtime artifact: %w", err)
	}

	return addFabricZipAmbientDependencies(ambient, zipRdr)
}

func addFabricZipAmbientDependencies(
	ambient *AmbientDependencies,
	zipRdr *zip.Reader,
) error {
	modInfo, err := readFabricModJSON(zipRdr)
	if err != nil || modInfo == nil {
		return err
	}

	id := types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Eco:  types.EcoFabric,
			Name: input.ToProjectName(modInfo.Id),
		},
		Version: types.BareVersion(modInfo.Version),
	}
	ambient.Add(id)
	for _, provided := range modInfo.Provides {
		// Fabric provides[] entries are aliases in the loader resolver, similar to
		// Debian/RPM virtual package provides. They satisfy dependency ids without
		// requiring a separate downloaded package.
		ambient.AddAlias(
			types.PackageRef{
				Eco:  types.EcoFabric,
				Name: input.ToProjectName(provided),
			},
			id,
		)
	}

	for _, nested := range modInfo.Jars {
		// Fabric Loader 0.15+ exposes MixinExtras this way; scan actual jars[] so
		// future loader-provided nested mods are discovered without a name list.
		raw, err := readZipEntry(zipRdr, nested.File)
		if err != nil {
			return err
		}
		if raw == nil {
			continue
		}
		nestedZip, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return err
		}
		if err := addFabricZipAmbientDependencies(
			ambient,
			nestedZip,
		); err != nil {
			return err
		}
	}

	return nil
}

func readFabricModJSON(
	zipRdr *zip.Reader,
) (*fileschema.FileFabricModIdentifier, error) {
	raw, err := readZipEntry(zipRdr, "fabric.mod.json")
	if err != nil || raw == nil {
		return nil, err
	}

	modInfo := &fileschema.FileFabricModIdentifier{}
	if err := json.Unmarshal(raw, modInfo); err != nil {
		return nil, err
	}
	return modInfo, nil
}

func currentJavaSpecVersion(ctx context.Context) (types.BareVersion, bool) {
	javaCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	output, err := exec.CommandContext(
		javaCtx,
		"java",
		"-XshowSettings:properties",
		"-version",
	).CombinedOutput()
	if err != nil && len(output) == 0 {
		return "", false
	}

	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "java.specification.version" {
			continue
		}
		version := strings.TrimSpace(value)
		version = strings.TrimPrefix(version, "1.")
		if version == "" {
			return "", false
		}
		return types.BareVersion(version), true
	}

	return "", false
}

func readZipEntry(zipRdr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zipRdr.File {
		if f.Name != name {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(r)
		if closeErr := r.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, nil
}
