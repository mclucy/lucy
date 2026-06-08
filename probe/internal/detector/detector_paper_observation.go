package detector

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/types"
)

const (
	paperMetaMainClassPath     = "META-INF/main-class"
	paperLibrariesListPath     = "META-INF/libraries.list"
	paperVersionsListPath      = "META-INF/versions.list"
	paperPatchesListPath       = "META-INF/patches.list"
	paperDownloadContextPath   = "META-INF/download-context"
	paperVersionJSONPath       = "version.json"
	paperPatchPropertiesPath   = "patch.properties"
	paperPatchReaperToken      = "paperMC.patch"
	paperPatchFilePath         = paperPatchReaperToken
	paperBuildInfoPath         = "META-INF/build-info"
	paperLeavesclipVersionPath = "META-INF/leavesclip-version"
	paperLeaperNamespacePrefix = "cn/dreeam/leaper/"
	paperLeavesclipNamespace   = "org/leavesmc/leavesclip/"
	paperPaperclipNamespace    = "io/papermc/paperclip/"
	paperLegacyPaperclipPrefix = "paperclip/"
	paperYouerNamespace        = "com/mohistmc/launcher/youer/"
	paperManifestYouerToken    = "Youer"
	paperMainClassYouerToken   = "youer"
	paperLibraryPaperToken     = "io.papermc.paper:paper-api:"
	paperLibraryFoliaToken     = "dev.folia:folia-api:"
	paperLibraryDivineToken    = "org.bxteam.divinemc:divinemc-api:"
	paperLibraryPurpurToken    = "org.purpurmc.purpur:purpur-api:"
	paperLibraryLeafToken      = "cn.dreeam.leaf:leaf-api:"
	paperLibraryLeavesToken    = "org.leavesmc.leaves:leaves-api:"
)

type paperObservationEntry struct {
	name string
	read func() ([]byte, error)
}

type paperObservationSource interface {
	Walk(func(paperObservationEntry) error) error
}

func extractPaperObservations(
filePath string,
zipReader *zip.Reader,
) (paperObservations, error) {
	obs := paperObservations{
		patchProperties: make(map[string]string),
	}

	source, err := newPaperObservationSource(filePath, zipReader)
	if err != nil {
		return paperObservations{}, err
	}

	err = source.Walk(
		func(entry paperObservationEntry) error {
			name := entry.name

			switch {
			case strings.HasPrefix(
				name,
				bukkitPaperClassPrefix,
			), strings.HasPrefix(name, bukkitLegacyPaperClassPrefix):
				obs.hasPaperClasses = true
			case strings.HasPrefix(name, bukkitSpigotClassPrefix):
				obs.hasSpigotClasses = true
			case strings.HasPrefix(name, paperLeaperNamespacePrefix):
				obs.hasLeaperNamespace = true
			case strings.HasPrefix(name, paperLeavesclipNamespace):
				obs.hasLeavesclipNamespace = true
			case strings.HasPrefix(name, paperPaperclipNamespace):
				obs.hasPaperclipNamespace = true
			case strings.HasPrefix(name, paperLegacyPaperclipPrefix):
				obs.hasLegacyPaperclipNamespace = true
			case strings.HasPrefix(name, paperYouerNamespace):
				obs.hasYouerNamespace = true
			}

			switch name {
			case bukkitManifestPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				signals := parseBukkitManifest(data)
				obs.manifestMainClass = signals.mainClass
				obs.manifestSpecificationTitle = signals.specificationTitle
				obs.manifestSpecificationVendor = signals.specificationVendor
				obs.manifestImplementationTitle = signals.implementationTitle
				obs.manifestImplementationVendor = signals.implementationVendor
				obs.manifestImplementationVer = signals.implementationVer
			case paperMetaMainClassPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				obs.metaMainClass = strings.TrimSpace(string(data))
			case paperLibrariesListPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				obs.librariesListEntries = readObservationLines(data)
			case paperVersionsListPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				obs.versionsListEntries = readObservationLines(data)
			case paperPatchesListPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				obs.patchesListEntries = readObservationLines(data)
			case paperDownloadContextPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				obs.downloadContext = strings.TrimSpace(string(data))
			case paperVersionJSONPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				obs.versionJSONID = parsePaperVersionJSONID(data)
			case paperPatchPropertiesPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				obs.patchProperties = parsePaperPatchProperties(data)
				obs.hasPaperMCPatch = obs.hasPaperMCPatch || strings.EqualFold(
					obs.patchProperties["patch"],
					paperPatchFilePath,
				)
			case paperPatchFilePath:
				obs.hasPaperMCPatch = true
			case paperBuildInfoPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				obs.buildInfo = strings.TrimSpace(string(data))
			case paperLeavesclipVersionPath:
				data, err := entry.read()
				if err != nil {
					return err
				}
				obs.leavesclipVersion = strings.TrimSpace(string(data))
			}

			return nil
		},
	)
	if err != nil {
		return paperObservations{}, err
	}

	obs.gameVersion = inferPaperObservationGameVersion(obs)
	return obs, nil
}

func newPaperObservationSource(
filePath string,
zipReader *zip.Reader,
) (paperObservationSource, error) {
	if zipReader != nil {
		return paperObservationZipSource{reader: zipReader}, nil
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf(
			"paper observation path is not a directory: %s",
			filePath,
		)
	}

	return paperObservationDirSource{root: filePath}, nil
}

type paperObservationZipSource struct {
	reader *zip.Reader
}

func (s paperObservationZipSource) Walk(fn func(paperObservationEntry) error) error {
	for _, file := range s.reader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		zipFile := file
		if err := fn(
			paperObservationEntry{
				name: filepath.ToSlash(zipFile.Name),
				read: func() ([]byte, error) {
					r, err := zipFile.Open()
					if err != nil {
						return nil, err
					}
					defer r.Close()
					return io.ReadAll(r)
				},
			},
		); err != nil {
			return err
		}
	}
	return nil
}

type paperObservationDirSource struct {
	root string
}

func (s paperObservationDirSource) Walk(fn func(paperObservationEntry) error) error {
	return filepath.WalkDir(
		s.root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(s.root, path)
			if err != nil {
				return err
			}
			absolutePath := path
			return fn(
				paperObservationEntry{
					name: filepath.ToSlash(rel),
					read: func() ([]byte, error) {
						return os.ReadFile(absolutePath)
					},
				},
			)
		},
	)
}

func readObservationLines(data []byte) []string {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func parsePaperVersionJSONID(data []byte) types.BareVersion {
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return types.VersionUnknown
	}
	if !isMinecraftReleaseVersion(payload.ID) {
		return types.VersionUnknown
	}
	return types.BareVersion(payload.ID)
}

func parsePaperPatchProperties(data []byte) map[string]string {
	properties := make(map[string]string)
	for _, line := range readObservationLines(data) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		properties[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return properties
}

func inferPaperObservationGameVersion(obs paperObservations) types.BareVersion {
	if hasConcreteVersion(obs.versionJSONID) {
		return obs.versionJSONID
	}
	if version := parseBukkitGameVersion(obs.manifestImplementationVer); hasConcreteVersion(version) {
		return version
	}
	if version := parsePaperVersionList(obs.versionsListEntries); hasConcreteVersion(version) {
		return version
	}
	if version := parsePaperPatchPropertiesVersion(obs.patchProperties); hasConcreteVersion(version) {
		return version
	}
	return types.VersionUnknown
}

func parsePaperVersionList(entries []string) types.BareVersion {
	for _, entry := range entries {
		fields := strings.Split(entry, "\t")
		if len(fields) < 2 {
			continue
		}
		candidate := strings.TrimSpace(fields[1])
		if isMinecraftReleaseVersion(candidate) {
			return types.BareVersion(candidate)
		}
	}
	return types.VersionUnknown
}

func parsePaperPatchPropertiesVersion(properties map[string]string) types.BareVersion {
	version := strings.TrimSpace(properties["version"])
	if !isMinecraftReleaseVersion(version) {
		return types.VersionUnknown
	}
	return types.BareVersion(version)
}
