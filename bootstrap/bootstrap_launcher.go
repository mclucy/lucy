package bootstrap

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/maven"
	serverpkg "github.com/mclucy/lucy/server"
)

// fabricLoaderPath returns the library path the Fabric method installs the
// locked loader to.
func fabricLoaderPath(version string) (string, error) {
	return loaderLibraryPath("net.fabricmc:fabric-loader", "Fabric", version)
}

// quiltLoaderPath returns the library path the Quilt method installs the
// locked loader to.
func quiltLoaderPath(version string) (string, error) {
	return loaderLibraryPath("org.quiltmc:quilt-loader", "Quilt", version)
}

// loaderLibraryPath returns the Maven-layout library path of a pinned loader.
func loaderLibraryPath(coordinate, loader, version string) (string, error) {
	if version == "" || version == "." || version == ".." || strings.ContainsAny(version, "/\\") {
		return "", fmt.Errorf("invalid %s loader version %q", loader, version)
	}
	p, err := maven.Path(coordinate + ":" + version)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("libraries", p)), nil
}

// loaderClasspath returns the launcher classpath: the loader library followed
// by every pinned library input in lock order.
func loaderClasspath(loaderPath string, placed []string) []string {
	classpath := make([]string, 1, 1+len(placed))
	classpath[0] = loaderPath
	for _, rel := range placed {
		if strings.HasPrefix(rel, "libraries/") {
			classpath = append(classpath, rel)
		}
	}
	return classpath
}

// forgeArgsPath returns the UniX launch arguments file generated for the
// locked Forge or NeoForge installation.
func forgeArgsPath(product serverpkg.Product, mcVersion, coreVersion string) string {
	return forgeVersionedPath(product, mcVersion, coreVersion, "unix_args.txt")
}

// forgeWinArgsPath returns the Windows launch arguments file generated for the
// locked Forge or NeoForge installation.
func forgeWinArgsPath(product serverpkg.Product, mcVersion, coreVersion string) string {
	return forgeVersionedPath(product, mcVersion, coreVersion, "win_args.txt")
}

// forgeVersionedPath returns the Maven-layout path of one generated file under
// the locked Forge or NeoForge version directory.
func forgeVersionedPath(product serverpkg.Product, mcVersion, coreVersion, name string) string {
	prefix := "net/neoforged/neoforge/"
	version := coreVersion
	if product == serverpkg.ProductForge {
		prefix = "net/minecraftforge/forge/"
		version = mcVersion + "-" + coreVersion
	}
	return "libraries/" + prefix + version + "/" + name
}

// forgeRootShimPath returns the server-relative shim a shim-era Forge
// installer writes at the server root and run.sh invokes for its Java check.
// NeoForge and earlier Forge branches write no root shim.
func forgeRootShimPath(locked document.LockedServer) string {
	if locked.Product != serverpkg.ProductForge {
		return ""
	}
	return "forge-" + locked.Minecraft.String() + "-" + locked.CoreVersion.String() + "-shim.jar"
}

// isForgeDiscardInput reports whether a locked Forge input exists only for
// installer processor execution.
func isForgeDiscardInput(cleanPath string) bool {
	return cleanPath == "server-mappings.txt" ||
		cleanPath == "server.jar" ||
		strings.HasPrefix(cleanPath, ".bootstrap-data/")
}
