package artifact

import (
	"archive/zip"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

// InstallerKind identifies the installer tool that produces an archive.
type InstallerKind string

const (
	InstallerForge    InstallerKind = "forge-installer"
	InstallerNeoForge InstallerKind = "neoforge-installer"
	InstallerFabric   InstallerKind = "fabric-installer"
	InstallerQuilt    InstallerKind = "quilt-installer"
)

// Installer is the declaration an installer archive carries about itself.
// LoaderVersion and Minecraft are empty for generic installers that do not
// declare a target.
type Installer struct {
	Kind          InstallerKind
	Product       server.Product
	LoaderVersion version.Version
	Minecraft     version.Version
	FilePath      string
}

const (
	installerProfilePath = "install_profile.json"
	installerVersionPath = "version.json"

	fabricInstallerMainClass = "net.fabricmc.installer.Main"
	quiltInstallerMainClass  = "org.quiltmc.installer.Main"
	fabricInstallerMainEntry = "net/fabricmc/installer/Main.class"
	quiltInstallerMainEntry  = "org/quiltmc/installer/Main.class"
)

// ReadInstaller identifies a supported installer archive from its contents.
// found is false for any archive that is not a supported installer.
//
// The archive name and extension are not evidence. A caller that scans a
// directory can log an error and continue: a non-archive or an archive without
// installer metadata yields no error.
func ReadInstaller(filePath string) (inst Installer, found bool, err error) {
	archive, err := zip.OpenReader(filePath)
	if err != nil {
		// A non-zip file is not an installer. Other open failures stay
		// visible to the caller.
		if errors.Is(err, zip.ErrFormat) {
			return Installer{}, false, nil
		}
		return Installer{}, false, fmt.Errorf("open installer archive: %w", err)
	}
	defer archive.Close()

	raw, err := readZipEntryBytes(&archive.Reader, installerProfilePath)
	if err != nil {
		return Installer{}, false, fmt.Errorf("read installer profile: %w", err)
	}
	if raw != nil {
		return readInstallerProfile(&archive.Reader, raw, filePath)
	}

	attributes, err := readManifestAttributes(
		&archive.Reader,
		fsutil.MaxZipEntryBytes,
	)
	if err != nil {
		return Installer{}, false, fmt.Errorf("read installer manifest: %w", err)
	}
	kind, product, identified := installerManifestKind(
		&archive.Reader,
		attributes,
	)
	if !identified {
		return Installer{}, false, nil
	}
	return Installer{Kind: kind, Product: product, FilePath: filePath}, true, nil
}

// installerProfile is an install_profile.json declaration. Spec 0 and spec 1
// use the same field names.
type installerProfile struct {
	Spec      int    `json:"spec"`
	Profile   string `json:"profile"`
	Version   string `json:"version"`
	Minecraft string `json:"minecraft"`
	JSON      string `json:"json"`
	Path      string `json:"path"`
}

// installerVersionProfile is the launcher profile that an install profile
// names. Only the fields that identify the target are relevant.
type installerVersionProfile struct {
	InheritsFrom string `json:"inheritsFrom"`
	Arguments    struct {
		Game []jsontext.Value `json:"game"`
	} `json:"arguments"`
}

func readInstallerProfile(
	zipRdr *zip.Reader,
	raw []byte,
	filePath string,
) (Installer, bool, error) {
	var profile installerProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return Installer{}, false, fmt.Errorf("decode installer profile: %w", err)
	}
	// Forge uses spec 1 for the modern declaration and spec 0 for the legacy
	// declaration.
	if profile.Spec != 0 && profile.Spec != 1 {
		return Installer{}, false, nil
	}
	product, ok := installerProduct(profile.Profile)
	if !ok {
		return Installer{}, false, nil
	}

	minecraft := profile.Minecraft
	versionProfile, err := readInstallerVersionProfile(zipRdr, profile.JSON)
	if err != nil {
		return Installer{}, false, err
	}
	if minecraft == "" && versionProfile != nil {
		minecraft = versionProfile.InheritsFrom
	}

	loaderVersion := installerLoaderVersion(
		profile,
		versionProfile,
		minecraft,
		product,
	)

	kind := installerKind(product)
	return Installer{
		Kind:          kind,
		Product:       product,
		LoaderVersion: loaderVersion,
		Minecraft:     version.Version(minecraft),
		FilePath:      filePath,
	}, true, nil
}

func installerProduct(name string) (server.Product, bool) {
	switch {
	case strings.EqualFold(name, "neoforge"):
		return server.ProductNeoForge, true
	case strings.EqualFold(name, "forge"):
		return server.ProductForge, true
	default:
		return server.ProductUnknown, false
	}
}

func installerKind(product server.Product) InstallerKind {
	switch product {
	case server.ProductForge:
		return InstallerForge
	case server.ProductNeoForge:
		return InstallerNeoForge
	case server.ProductFabric:
		return InstallerFabric
	case server.ProductQuilt:
		return InstallerQuilt
	default:
		return ""
	}
}

// readInstallerVersionProfile reads the launcher profile that the install
// profile names. A missing entry is not an error because legacy profiles keep
// the same data in install_profile.json.
func readInstallerVersionProfile(
	zipRdr *zip.Reader,
	name string,
) (*installerVersionProfile, error) {
	if name == "" {
		name = installerVersionPath
	}
	name = strings.TrimPrefix(name, "/")
	raw, err := readZipEntryBytes(zipRdr, name)
	if err != nil {
		return nil, fmt.Errorf("read installer version profile: %w", err)
	}
	if raw == nil {
		return nil, nil
	}
	var profile installerVersionProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return nil, fmt.Errorf("decode installer version profile: %w", err)
	}
	return &profile, nil
}

// installerLoaderVersion resolves the loader version in declaration order:
// launch arguments, the declared profile version, then Maven coordinates. A
// source that names no concrete version does not stop the search.
func installerLoaderVersion(
	profile installerProfile,
	versionProfile *installerVersionProfile,
	minecraft string,
	product server.Product,
) version.Version {
	candidates := []string{
		installerArgumentValue(versionProfile, installerLoaderArgument(product)),
		installerVersionFromDeclaration(profile.Version, product),
		installerVersionFromCoordinates(profile.Path, minecraft, product),
	}
	for _, candidate := range candidates {
		if concrete := concreteInstallerVersion(candidate); concrete != "" {
			return concrete
		}
	}
	return ""
}

func installerLoaderArgument(product server.Product) string {
	switch product {
	case server.ProductNeoForge:
		return "--fml.neoForgeVersion"
	case server.ProductForge:
		return "--fml.forgeVersion"
	default:
		return ""
	}
}

// installerArgumentValue returns the token after name. The launcher stores
// each argument as a separate string, and an object argument carries no
// loader version, so this scan ignores tokens that are not strings.
func installerArgumentValue(
	profile *installerVersionProfile,
	name string,
) string {
	if profile == nil || name == "" {
		return ""
	}
	tokens := make([]string, 0, len(profile.Arguments.Game))
	for _, raw := range profile.Arguments.Game {
		var token string
		if err := json.Unmarshal(raw, &token); err != nil {
			continue
		}
		tokens = append(tokens, token)
	}
	for index := 0; index+1 < len(tokens); index++ {
		if tokens[index] == name {
			return tokens[index+1]
		}
	}
	return ""
}

// installerVersionFromDeclaration parses the loader version from the id that
// the installer declares. NeoForge ids are neoforge-<v>. Modern Forge ids are
// <minecraft>-forge-<v>. Legacy ids use the path field instead.
func installerVersionFromDeclaration(
	declared string,
	product server.Product,
) string {
	declared = strings.TrimSpace(declared)
	switch product {
	case server.ProductNeoForge:
		const prefix = "neoforge-"
		if len(declared) > len(prefix) &&
			strings.EqualFold(declared[:len(prefix)], prefix) {
			return declared[len(prefix):]
		}
	case server.ProductForge:
		const infix = "-forge-"
		if index := strings.Index(strings.ToLower(declared), infix); index >= 0 {
			return declared[index+len(infix):]
		}
	}
	return ""
}

// installerVersionFromCoordinates parses the loader version from a Maven
// coordinate in the path field: group:artifact:version[:classifier]. Forge
// writes the version as <minecraft>-<loader>; NeoForge writes the loader
// version alone.
func installerVersionFromCoordinates(
	coordinates string,
	minecraft string,
	product server.Product,
) string {
	parts := strings.Split(coordinates, ":")
	if len(parts) < 3 {
		return ""
	}
	versionPart := parts[2]
	if len(parts) > 3 {
		versionPart = parts[len(parts)-2]
	}
	if minecraft != "" {
		versionPart = strings.TrimPrefix(versionPart, minecraft+"-")
	} else if product == server.ProductForge {
		if index := strings.IndexByte(versionPart, '-'); index >= 0 {
			versionPart = versionPart[index+1:]
		}
	}
	return versionPart
}

// concreteInstallerVersion rejects values that name no exact version.
func concreteInstallerVersion(raw string) version.Version {
	concrete := version.Version(strings.TrimSpace(raw))
	if concrete == "" || concrete.IsInvalid() {
		return ""
	}
	return concrete
}

// installerManifestKind identifies a generic Fabric or Quilt installer. Both
// the main class and its class file must match, so a mod that only copies the
// manifest attribute does not qualify.
func installerManifestKind(
	zipRdr *zip.Reader,
	attributes map[string]string,
) (InstallerKind, server.Product, bool) {
	switch mainClass := manifestAttribute(attributes, "Main-Class"); mainClass {
	case fabricInstallerMainClass:
		if archiveContainsEntry(zipRdr, fabricInstallerMainEntry) {
			return InstallerFabric, server.ProductFabric, true
		}
	case quiltInstallerMainClass:
		if archiveContainsEntry(zipRdr, quiltInstallerMainEntry) {
			return InstallerQuilt, server.ProductQuilt, true
		}
	}
	return "", server.ProductUnknown, false
}

func archiveContainsEntry(zipRdr *zip.Reader, name string) bool {
	for _, file := range zipRdr.File {
		if file.Name == name {
			return true
		}
	}
	return false
}
