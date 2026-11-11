package artifact

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
)

// SupportsPath reports whether path has a supported artifact archive
// extension.
func SupportsPath(path string) bool {
	return supportedExt(strings.ToLower(filepath.Ext(path)))
}

func supportedExt(ext string) bool {
	switch ext {
	case ".jar", ".zip", ".pyz", ".mcdr":
		return true
	default:
		return false
	}
}

// ReadInfo extracts package metadata from an artifact file. For .jar/.zip
// files all readers are tried; for .pyz/.mcdr only the MCDR reader runs. An
// archive may carry descriptors for several ecosystems, so callers select the
// runtime identity with PreferredPackageInfos.
func ReadInfo(filePath string) ([]PackageInfo, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if !supportedExt(ext) {
		return nil, fmt.Errorf("unsupported artifact format: %s", ext)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat artifact: %w", err)
	}

	zipReader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}

	var results []PackageInfo

	if ext == ".pyz" || ext == ".mcdr" {
		mcdrReader := newMcdrReader()
		return mcdrReader.Read(zipReader, filePath)
	}

	for _, r := range readers {
		infos, err := r.Read(zipReader, filePath)
		if err != nil {
			continue
		}
		results = append(results, infos...)
	}
	if jarEcosystemsConflict(results) {
		return nil, fmt.Errorf(
			"ambiguous artifact %q: packages span incompatible ecosystems",
			filePath,
		)
	}

	return results, nil
}

// PreferredPackageInfos keeps the identities a runtime loads. An unspecified
// runtime keeps the loader identity that loads every other descriptor present,
// such as Quilt for an artifact that also carries fabric.mod.json.
func PreferredPackageInfos(
	infos []PackageInfo,
	runtime catalog.Ecosystem,
) []PackageInfo {
	if len(infos) <= 1 {
		return infos
	}
	if runtime == catalog.EcosystemUnspecified {
		runtime = loaderIdentity(infos)
		if runtime == catalog.EcosystemUnspecified {
			return infos
		}
	}
	for _, ecosystem := range runtime.Loads() {
		selected := slices.DeleteFunc(
			append([]PackageInfo(nil), infos...),
			func(info PackageInfo) bool {
				return info.Ref.Ecosystem != ecosystem
			},
		)
		if len(selected) > 0 {
			return selected
		}
	}
	return infos
}

// loaderIdentity returns the ecosystem whose Loads list covers every ecosystem
// the artifact declares, or unspecified when none does.
func loaderIdentity(infos []PackageInfo) catalog.Ecosystem {
	present := make([]catalog.Ecosystem, 0, len(infos))
	for _, info := range infos {
		if !slices.Contains(present, info.Ref.Ecosystem) {
			present = append(present, info.Ref.Ecosystem)
		}
	}
	if len(present) < 2 {
		return catalog.EcosystemUnspecified
	}

	var candidate catalog.Ecosystem
	for _, ecosystem := range present {
		covers := true
		for _, other := range present {
			if other != ecosystem && !ecosystem.Satisfy(other) {
				covers = false
				break
			}
		}
		if !covers {
			continue
		}
		if candidate != catalog.EcosystemUnspecified {
			return catalog.EcosystemUnspecified
		}
		candidate = ecosystem
	}
	return candidate
}

// jarEcosystemsConflict returns true when the detected artifacts span two or
// more ecosystem families that cannot coexist in a single deployable JAR.
func jarEcosystemsConflict(infos []PackageInfo) bool {
	families := make(map[catalog.Family]struct{}, len(infos))
	for _, info := range infos {
		family := info.Ref.Ecosystem.Family()
		if family == catalog.FamilyNone {
			continue
		}
		families[family] = struct{}{}
	}
	return len(families) > 1
}
