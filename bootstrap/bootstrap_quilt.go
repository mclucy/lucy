package bootstrap

import (
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
)

const (
	// quiltLauncherMainClass is the entry point of the generated Quilt launcher JAR.
	quiltLauncherMainClass = "org.quiltmc.loader.impl.launch.server.QuiltServerLauncher"
	// quiltKnotMainClass is the server main class the launcher runs through its
	// embedded launch properties.
	quiltKnotMainClass = "org.quiltmc.loader.impl.launch.knot.KnotServer"
)

// prepareQuilt places the Quilt loader on the launcher classpath and generates
// the launcher JAR and the properties that select the vanilla server JAR.
func prepareQuilt(root string, server document.LockedServer, corePath string) error {
	loaderPath, err := quiltLoaderPath(server.CoreVersion.String())
	if err != nil {
		return err
	}
	if err := fsutil.AtomicCopyFile(corePath, filepath.Join(root, filepath.FromSlash(loaderPath)), 0o644); err != nil {
		return err
	}
	classpath := loaderClasspath(loaderPath, server.Inputs)
	if err := writeLauncherShimJar(
		filepath.Join(root, "quilt-server-launch.jar"),
		"quilt-server-launch.properties",
		quiltLauncherMainClass,
		quiltKnotMainClass,
		classpath,
	); err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(root, "quilt-server-launcher.properties"),
		[]byte("serverJar=server.jar\n"),
		0o644,
	)
}
