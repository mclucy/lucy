package discovery

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mclucy/lucy/server"
)

// Discover scans one server directory through the static directory recognizer
// registry and reconciles every recognizer output into a single observation.
// It does not read workspace declarations and does not choose a runtime; call
// Select afterwards.
func Discover(serverDir string) (result Result) {
	result.Selection = Selection{State: SelectionNone}
	scan, err := newScan(serverDir)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Finding{
			Kind:  FindingUnreadableFile,
			Path:  serverDir,
			Cause: err,
		})
		return result
	}
	defer func() {
		if closeErr := scan.Close(); closeErr != nil {
			result.Diagnostics = append(result.Diagnostics, Finding{
				Kind:  FindingUnreadableFile,
				Path:  scan.root,
				Cause: closeErr,
			})
		}
	}()

	raw := make([]Installation, 0, len(recognizerRegistry))
	for _, entry := range recognizerRegistry {
		installations, findings := entry.discover(scan)
		raw = append(raw, installations...)
		result.Diagnostics = append(result.Diagnostics, findings...)
	}
	installations, findings := reconcileInstallations(raw, scan)
	result.Installations = installations
	result.Diagnostics = append(result.Diagnostics, findings...)
	return result
}

func reconcileInstallations(
	raw []Installation,
	scan *scan,
) ([]Installation, []Finding) {
	var findings []Finding
	installations := make([]Installation, 0, len(raw))
	for index := range raw {
		installation := raw[index]
		if len(installation.Files) == 0 {
			continue
		}
		if err := installation.Identity.Normalize(); err != nil {
			path := firstEntryPointPath(&installation)
			if path == "" {
				path = firstFilePath(installation)
			}
			findings = append(findings, Finding{
				Kind:     FindingInvalidIdentity,
				Path:     path,
				Identity: installation.Identity,
				Cause:    err,
			})
		}
		installations = append(installations, installation)
	}

	installations = shadowVanillaClaims(installations)
	installations = mergeIdenticalInstallations(installations)
	installations = absorbVanillaPayloads(installations)
	installations = dropEmptyInstallations(installations)
	findings = append(findings, conflictingClaims(installations)...)
	findings = append(findings, unknownArtifacts(installations, scan)...)
	sort.Slice(installations, func(i, j int) bool {
		return compareInstallations(installations[i], installations[j]) < 0
	})
	return installations, findings
}

// shadowVanillaClaims drops a generic vanilla entry-point claim when a
// specific installation claims the same artifact. Other vanilla entry points
// on the same installation remain observable.
func shadowVanillaClaims(installations []Installation) []Installation {
	claims := make(map[string][]int, len(installations))
	for index := range installations {
		if !installations[index].Identity.IsValid() {
			continue
		}
		for file := range installations[index].EntryPoints() {
			claims[file.Path] = append(claims[file.Path], index)
		}
	}
	shadowed := make(map[int]map[string]bool)
	for path, indexes := range claims {
		specific := false
		for _, index := range indexes {
			identity := installations[index].Identity
			if !isVanillaIdentity(identity) && identity.IsValid() {
				specific = true
				break
			}
		}
		if !specific {
			continue
		}
		for _, index := range indexes {
			if !isVanillaIdentity(installations[index].Identity) {
				continue
			}
			if shadowed[index] == nil {
				shadowed[index] = make(map[string]bool)
			}
			shadowed[index][path] = true
		}
	}
	out := installations[:0]
	for index := range installations {
		for path := range shadowed[index] {
			installations[index].removeFile(path)
		}
		if len(installations[index].Files) == 0 {
			continue
		}
		out = append(out, installations[index])
	}
	clear(installations[len(out):])
	return out
}

// mergeIdenticalInstallations unions members and entry points of equal
// concrete identities. Unknown or contradictory identities never merge.
func mergeIdenticalInstallations(installations []Installation) []Installation {
	out := make([]Installation, 0, len(installations))
	indexes := make(map[string]int, len(installations))
	for index := range installations {
		candidate := installations[index]
		if candidate.Identity.IsValid() && candidate.Identity.IsConcrete() {
			key := identityKey(candidate.Identity)
			if existing, ok := indexes[key]; ok &&
				out[existing].Identity.Equal(candidate.Identity) {
				for _, file := range candidate.Files {
					out[existing].addFile(file)
				}
				continue
			}
			indexes[key] = len(out)
		}
		out = append(out, candidate)
	}
	return out
}

// absorbVanillaPayloads treats a vanilla entry point referenced as a member by
// a matching-Minecraft non-vanilla installation as payload, not as a second
// standalone installation.
func absorbVanillaPayloads(installations []Installation) []Installation {
	type reference struct {
		installation int
		path         string
	}
	drop := make([]bool, len(installations))
	for vanillaIndex := range installations {
		vanilla := &installations[vanillaIndex]
		if !vanilla.Identity.IsValid() || !isVanillaIdentity(vanilla.Identity) {
			continue
		}
		minecraft := vanilla.Identity.MinecraftVersion()
		if !concreteVersion(minecraft) {
			continue
		}
		referenced := make(map[string]bool)
		var references []reference
		for file := range vanilla.EntryPoints() {
			for installerIndex := range installations {
				if installerIndex == vanillaIndex {
					continue
				}
				installer := &installations[installerIndex]
				if !installer.Identity.IsValid() ||
					isVanillaIdentity(installer.Identity) ||
					firstEntryPoint(installer) == nil {
					continue
				}
				installerMinecraft := installer.Identity.MinecraftVersion()
				if !concreteVersion(installerMinecraft) ||
					installerMinecraft != minecraft ||
					!hasFile(installer, file.Path) {
					continue
				}
				references = append(references, reference{
					installation: installerIndex,
					path:         file.Path,
				})
				referenced[file.Path] = true
			}
		}
		if len(references) == 0 {
			continue
		}
		for _, found := range references {
			member := fileAt(vanilla, found.path)
			if member == nil {
				continue
			}
			installations[found.installation].addFile(File{
				Path:      member.Path,
				Role:      RolePayload,
				Ownership: member.Ownership,
			})
		}
		allReferenced := true
		for file := range vanilla.EntryPoints() {
			if !referenced[file.Path] {
				allReferenced = false
				break
			}
		}
		if allReferenced {
			for _, found := range references {
				for _, file := range vanilla.Files {
					role := file.Role
					if role == RoleEntryPoint {
						role = RolePayload
					}
					installations[found.installation].addFile(File{
						Path:      file.Path,
						Role:      role,
						Ownership: file.Ownership,
					})
				}
			}
			drop[vanillaIndex] = true
			continue
		}
		for path := range referenced {
			vanilla.removeFile(path)
		}
	}
	out := installations[:0]
	for index := range installations {
		if drop[index] {
			continue
		}
		out = append(out, installations[index])
	}
	clear(installations[len(out):])
	return out
}

func conflictingClaims(installations []Installation) []Finding {
	claims := make(map[string][]*Installation, len(installations))
	for index := range installations {
		installation := &installations[index]
		if !installation.Identity.IsValid() {
			continue
		}
		for file := range installation.EntryPoints() {
			claims[file.Path] = append(claims[file.Path], installation)
		}
	}
	paths := make([]string, 0, len(claims))
	for path := range claims {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	findings := make([]Finding, 0)
	for _, path := range paths {
		found := claims[path]
		if len(found) <= 1 || sameConcreteIdentity(found) {
			continue
		}
		identities := make([]Identity, 0, len(found))
		entryPoints := make([]string, 0, len(found))
		for _, installation := range found {
			identities = append(identities, installation.Identity)
			for file := range installation.EntryPoints() {
				entryPoints = append(entryPoints, file.Path)
			}
		}
		findings = append(findings, Finding{
			Kind:       FindingConflictingIdentities,
			Path:       path,
			Paths:      sortedUnique(entryPoints),
			Identities: uniqueIdentities(identities),
		})
	}
	return findings
}

func sameConcreteIdentity(installations []*Installation) bool {
	if len(installations) == 0 {
		return true
	}
	first := installations[0].Identity
	if !first.IsConcrete() {
		return false
	}
	for _, installation := range installations[1:] {
		if !installation.Identity.IsConcrete() ||
			!first.Equal(installation.Identity) {
			return false
		}
	}
	return true
}

func unknownArtifacts(installations []Installation, scan *scan) []Finding {
	if scan == nil {
		return nil
	}
	claimed := make(map[string]bool)
	for index := range installations {
		for _, file := range installations[index].Files {
			claimed[filepath.Clean(file.Path)] = true
		}
	}
	var findings []Finding
	for _, artifact := range scan.RootFiles() {
		if artifact.IsDirectory() ||
			!strings.HasSuffix(strings.ToLower(artifact.Path()), ".jar") ||
			claimed[filepath.Clean(artifact.Path())] {
			continue
		}
		findings = append(findings, Finding{
			Kind: FindingUnknownArtifact,
			Path: artifact.Path(),
		})
	}
	return findings
}

func dropEmptyInstallations(installations []Installation) []Installation {
	out := installations[:0]
	for index := range installations {
		if len(installations[index].Files) == 0 {
			continue
		}
		out = append(out, installations[index])
	}
	clear(installations[len(out):])
	return out
}

func compareInstallations(left, right Installation) int {
	if result := compareIdentities(left.Identity, right.Identity); result != 0 {
		return result
	}
	if result := strings.Compare(firstEntryPointPath(&left), firstEntryPointPath(&right)); result != 0 {
		return result
	}
	return strings.Compare(firstFilePath(left), firstFilePath(right))
}

func firstFilePath(installation Installation) string {
	if len(installation.Files) == 0 {
		return ""
	}
	path := installation.Files[0].Path
	for _, file := range installation.Files[1:] {
		if file.Path < path {
			path = file.Path
		}
	}
	return path
}

func isVanillaIdentity(identity Identity) bool {
	if !identity.IsValid() {
		return false
	}
	return identity.Product() == server.ProductMinecraft
}

func hasFile(installation *Installation, path string) bool {
	return fileAt(installation, path) != nil
}

func fileAt(installation *Installation, path string) *File {
	if installation == nil {
		return nil
	}
	path = filepath.Clean(path)
	for index := range installation.Files {
		if installation.Files[index].Path == path {
			return &installation.Files[index]
		}
	}
	return nil
}
