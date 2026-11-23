package bootstrap

import (
	"fmt"
	"path"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/maven"
	serverpkg "github.com/mclucy/lucy/server"
)

// CorePath returns the server-relative path where the method keeps the core
// artifact unchanged, or an empty path when it keeps no copy.
func CorePath(locked document.LockedServer) (string, error) {
	switch locked.Bootstrap {
	case serverpkg.BootstrapMethodVanilla, serverpkg.BootstrapMethodPaperclip:
		return "server.jar", nil
	case serverpkg.BootstrapMethodFabric:
		return fabricLoaderPath(locked.CoreVersion.String())
	case serverpkg.BootstrapMethodQuilt:
		return quiltLoaderPath(locked.CoreVersion.String())
	case serverpkg.BootstrapMethodForge:
		return "", nil
	default:
		return "", locked.Bootstrap.Validate()
	}
}

// InputPath places one locked input in the server tree of method. The lock
// records what an input is; this function owns where it lands. Libraries
// follow the Maven layout under libraries/. The Mojang server jar lands where
// the method's launcher or patcher reads it.
func InputPath(method serverpkg.BootstrapMethod, input catalog.Artifact) (string, error) {
	var placed string
	switch {
	case input.Provider == catalog.ProviderMaven:
		p, err := maven.Path(input.ProjectID)
		if err != nil {
			return "", err
		}
		placed = path.Join("libraries", p)
		if !strings.HasPrefix(placed, "libraries/") {
			return "", fmt.Errorf("library %s escapes libraries/", input.ProjectID)
		}
	case input.Provider == catalog.ProviderMojang && input.ProjectID == "minecraft":
		placed = "server.jar"
		if method == serverpkg.BootstrapMethodPaperclip {
			// Paperclip reads the Mojang jar from the cache name its download
			// context declares; routing verifies that name matches this one.
			v := input.Version.String()
			if v == "" || strings.ContainsAny(v, "/\\") || strings.Contains(v, "..") {
				return "", fmt.Errorf("invalid Mojang server version %q", v)
			}
			placed = "cache/mojang_" + v + ".jar"
		}
	case input.Provider == catalog.ProviderMojang && input.ProjectID == "server-mappings":
		placed = "server-mappings.txt"
	default:
		return "", fmt.Errorf(
			"bootstrap input %s:%s has no place in a %s server tree",
			input.Provider, input.ProjectID, method,
		)
	}
	if err := document.SafeRelativePath(placed); err != nil {
		return "", err
	}
	return placed, nil
}

// inputPaths places every locked input of server in input order.
func inputPaths(server document.LockedServer) ([]string, error) {
	paths := make([]string, len(server.Inputs))
	seen := make(map[string]bool, len(server.Inputs))
	for i, input := range server.Inputs {
		placed, err := InputPath(server.Bootstrap, input)
		if err != nil {
			return nil, err
		}
		if seen[placed] {
			return nil, fmt.Errorf("two locked inputs land at %s", placed)
		}
		seen[placed] = true
		paths[i] = placed
	}
	return paths, nil
}
