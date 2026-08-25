package install

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/artifact"
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

// Ambient dependencies are exact dependency identities already contributed by
// observed runtime components or environments before Lucy resolves requests.
func buildAmbientDependencies(
	_ context.Context,
	ws workspace.Workspace,
) (AmbientDependencies, error) {
	ambient := AmbientDependencies{
		entries: make(map[string]types.VersionedPackageRef),
		aliases: make(map[string]string),
	}
	if ws.Environments.Mcdr != nil {
		ambient.Add(
			types.VersionedPackageRef{
				Eco:     types.EcoMcdr,
				Name:    "mcdreforged",
				Version: types.BareVersion(strings.TrimSpace(ws.Environments.Mcdr.Version.String())),
			},
		)
	}

	server := ws.Server()
	if server == nil {
		return ambient, nil
	}

	fabricLoaderVersion := addRuntimeComponentAmbientDependencies(
		&ambient,
		server.RuntimeComponents,
	)
	if fabricLoaderVersion == "" {
		return ambient, nil
	}

	loaderPath := fabricLoaderArtifactPath(ws.Root, fabricLoaderVersion)
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

func addRuntimeComponentAmbientDependencies(
	ambient *AmbientDependencies,
	components []types.VersionedPackageRef,
) string {
	var minecraft types.VersionedPackageRef
	var fabricLoaderVersion string
	var hasFabricLoader bool
	var hasForge bool
	var hasNeoForge bool

	for _, component := range components {
		if !concreteAmbientVersion(component.Version) {
			continue
		}

		name := strings.ToLower(strings.TrimSpace(component.Name.String()))
		switch {
		case component.Eco == types.EcoMinecraft && name == "minecraft":
			minecraft = component
			ambient.Add(component)
		case component.Eco == types.EcoFabric &&
			(name == "fabric-loader" || name == "fabricloader"):
			loader := component
			loader.Name = "fabric-loader"
			ambient.Add(loader)
			ambient.AddAlias(
				types.PackageRef{
					Eco:  types.EcoFabric,
					Name: "fabricloader",
				},
				loader,
			)
			fabricLoaderVersion = loader.Version.String()
			hasFabricLoader = true
		case component.Eco == types.EcoFabric && name == "fabric-api":
			ambient.Add(component)
		case component.Eco == types.EcoForge && name == "forge":
			ambient.Add(component)
			hasForge = true
		case component.Eco == types.EcoNeoforge && name == "neoforge":
			ambient.Add(component)
			hasNeoForge = true
		}
	}

	if minecraft.PackageRef != (types.PackageRef{}) {
		if hasFabricLoader {
			ambient.AddAlias(
				types.PackageRef{
					Eco:  types.EcoFabric,
					Name: "minecraft",
				},
				minecraft,
			)
		}
		if hasForge {
			ambient.AddAlias(
				types.PackageRef{
					Eco:  types.EcoForge,
					Name: "minecraft",
				},
				minecraft,
			)
		}
		if hasNeoForge {
			ambient.AddAlias(
				types.PackageRef{
					Eco:  types.EcoNeoforge,
					Name: "minecraft",
				},
				minecraft,
			)
		}
	}

	return fabricLoaderVersion
}

func concreteAmbientVersion(version types.BareVersion) bool {
	return version != "" && !version.IsInvalid() && !version.CanInfer()
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
		Eco:     types.EcoFabric,
		Name:    input.ToProjectName(modInfo.Id),
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
		raw, err := artifact.ReadZipEntryBytes(zipRdr, nested.File)
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
	raw, err := artifact.ReadZipEntryBytes(zipRdr, "fabric.mod.json")
	if err != nil || raw == nil {
		return nil, err
	}

	modInfo := &fileschema.FileFabricModIdentifier{}
	if err := json.Unmarshal(raw, modInfo); err != nil {
		return nil, err
	}
	return modInfo, nil
}
