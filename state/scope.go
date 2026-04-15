package state

import (
	"path"
	"strings"
)

type ArtifactClass string

const (
	ClassMod          ArtifactClass = "mod"
	ClassPlugin       ArtifactClass = "plugin"
	ClassConfig       ArtifactClass = "config"
	ClassDatapack     ArtifactClass = "datapack"
	ClassResourcePack ArtifactClass = "resourcepack"
	ClassKubeJS       ArtifactClass = "kubejs"
	ClassEmbedded     ArtifactClass = "embedded"
	ClassUnmanaged    ArtifactClass = "unmanaged"
)

type ManagedRoot struct {
	Path  string
	Class ArtifactClass
}

type ManagedScope struct {
	Roots             []ManagedRoot
	UnmanagedPatterns []string
}

func DefaultManagedRoots() []ManagedRoot {
	return []ManagedRoot{
		{Path: "mods", Class: ClassMod},
		{Path: "plugins", Class: ClassPlugin},
		{Path: "config", Class: ClassConfig},
	}
}

func NewManagedScope(roots []string, unmanagedPatterns []string) ManagedScope {
	if len(roots) == 0 {
		defaults := DefaultManagedRoots()
		return ManagedScope{Roots: defaults, UnmanagedPatterns: normalizePatterns(unmanagedPatterns)}
	}

	managedRoots := make([]ManagedRoot, 0, len(roots))
	for _, root := range roots {
		normalized := normalizeRelativePath(root)
		if normalized == "." || normalized == "" {
			continue
		}
		managedRoots = append(managedRoots, ManagedRoot{
			Path:  normalized,
			Class: classifyManagedRoot(normalized),
		})
	}

	return ManagedScope{Roots: managedRoots, UnmanagedPatterns: normalizePatterns(unmanagedPatterns)}
}

func IsManaged(scope ManagedScope, relPath string) bool {
	normalized := normalizeRelativePath(relPath)
	if normalized == "." || normalized == "" {
		return false
	}
	if matchesAnyPattern(scope.UnmanagedPatterns, normalized) {
		return false
	}
	_, ok := managedRootForPath(scope, normalized)
	return ok
}

func ClassifyPath(scope ManagedScope, relPath string) ArtifactClass {
	normalized := normalizeRelativePath(relPath)
	if normalized == "." || normalized == "" {
		return ClassUnmanaged
	}
	if !IsManaged(scope, normalized) {
		return ClassUnmanaged
	}
	root, ok := managedRootForPath(scope, normalized)
	if !ok {
		return ClassUnmanaged
	}
	return root.Class
}

func normalizePatterns(patterns []string) []string {
	normalized := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		value := normalizeRelativePath(pattern)
		if value == "." || value == "" {
			continue
		}
		normalized = append(normalized, value)
	}
	return normalized
}

func managedRootForPath(scope ManagedScope, relPath string) (ManagedRoot, bool) {
	for _, root := range scope.Roots {
		if pathWithinRoot(relPath, root.Path) {
			return root, true
		}
	}
	return ManagedRoot{}, false
}

func classifyManagedRoot(root string) ArtifactClass {
	switch {
	case pathWithinRoot(root, "mods"):
		return ClassMod
	case pathWithinRoot(root, "plugins"):
		return ClassPlugin
	case pathWithinRoot(root, "config"):
		return ClassConfig
	case strings.Contains(root, "datapacks"):
		return ClassDatapack
	case pathWithinRoot(root, "resourcepacks"):
		return ClassResourcePack
	case pathWithinRoot(root, "kubejs"):
		return ClassKubeJS
	default:
		return ClassConfig
	}
}

func matchesAnyPattern(patterns []string, relPath string) bool {
	for _, pattern := range patterns {
		if globMatch(pattern, relPath) {
			return true
		}
	}
	return false
}

func globMatch(pattern string, relPath string) bool {
	pattern = normalizeRelativePath(pattern)
	relPath = normalizeRelativePath(relPath)

	if pattern == relPath {
		return true
	}
	if prefix, ok := strings.CutSuffix(pattern, "/**"); ok {
		return pathWithinRoot(relPath, prefix)
	}
	matched, err := path.Match(pattern, relPath)
	return err == nil && matched
}

func pathWithinRoot(relPath string, root string) bool {
	relPath = normalizeRelativePath(relPath)
	root = normalizeRelativePath(root)
	if relPath == root {
		return true
	}
	return strings.HasPrefix(relPath, root+"/")
}

func normalizeRelativePath(value string) string {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if trimmed == "" {
		return ""
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." {
		return cleaned
	}
	return strings.TrimPrefix(cleaned, "./")
}
