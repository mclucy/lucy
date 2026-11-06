package bootstrap

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/maven"
	serverpkg "github.com/mclucy/lucy/server"
)

// forgeShimMainClass is the entry point of the Forge bootstrap shim that a
// shim-era installer writes.
const forgeShimMainClass = "net.minecraftforge.bootstrap.shim.Main"

// launcherCheck is the one verification result of a generated launcher. Stage
// and existing-tree verification both consume it, so each launcher file is read
// once and the root shim is attributed the same way on both paths.
type launcherCheck struct {
	// rootShim is the server-relative root shim that run.sh names. It is set
	// whenever run.sh names the shim, even when the shim fails verification, so
	// staging does not misreport it as unpermitted output. Only a nil err admits
	// it as an output.
	rootShim string
	// err is the first launcher verification failure, or nil.
	err error
}

// verifyLauncher checks the generated launch files of a bootstrap method.
// Methods without a generated launcher have nothing to check here.
func verifyLauncher(serverDir string, locked document.LockedServer) launcherCheck {
	switch locked.Bootstrapper {
	case serverpkg.BootstrapMethodFabric:
		return launcherCheck{err: verifyFabricLauncher(serverDir, locked)}
	case serverpkg.BootstrapMethodForgeInstaller:
		rootShim, err := verifyForgeLauncher(serverDir, locked)
		return launcherCheck{rootShim: rootShim, err: err}
	default:
		return launcherCheck{}
	}
}

// verifyFabricLauncher checks the Fabric launch scripts, launcher properties,
// and shim JAR against the locked classpath.
func verifyFabricLauncher(serverDir string, locked document.LockedServer) error {
	for _, f := range []string{"fabric-server-launcher.properties", "run.sh", "run.bat", "fabric-server-launch.jar"} {
		if err := checkPathSafety(serverDir, f, true); err != nil {
			return err
		}
	}

	propsPath := filepath.Join(serverDir, "fabric-server-launcher.properties")
	propsData, err := readBoundedFile(propsPath, 1<<20)
	if err != nil {
		return fmt.Errorf("reading fabric-server-launcher.properties: %w", err)
	}
	if strings.TrimSpace(string(propsData)) != "serverJar=server.jar" {
		return errors.New("fabric-server-launcher.properties does not match expected serverJar=server.jar")
	}

	loaderPath, err := fabricLoaderPath(locked.CoreVersion.String())
	if err != nil {
		return fmt.Errorf("resolving fabric loader path: %w", err)
	}
	classpath := []string{loaderPath}
	for _, input := range locked.Inputs {
		if strings.HasPrefix(input.Path, "libraries/") {
			classpath = append(classpath, input.Path)
		}
	}
	unixClasspath := strings.Join(classpath, ":")
	windowsClasspath := strings.Join(classpath, ";")

	runShPath := filepath.Join(serverDir, "run.sh")
	runShData, err := readBoundedFile(runShPath, 1<<20)
	if err != nil {
		return fmt.Errorf("reading run.sh: %w", err)
	}
	expectedRunSh := "#!/bin/sh\ncd -- \"$(dirname -- \"$0\")\"\nexec java -cp '" + unixClasspath + "' net.fabricmc.loader.impl.launch.server.FabricServerLauncher nogui \"$@\"\n"
	if normalizeNewlines(string(runShData)) != normalizeNewlines(expectedRunSh) {
		return errors.New("run.sh does not match expected Fabric launch script")
	}

	runBatPath := filepath.Join(serverDir, "run.bat")
	runBatData, err := readBoundedFile(runBatPath, 1<<20)
	if err != nil {
		return fmt.Errorf("reading run.bat: %w", err)
	}
	expectedRunBat := "@echo off\r\ncd /d %~dp0\r\njava -cp \"" + windowsClasspath + "\" net.fabricmc.loader.impl.launch.server.FabricServerLauncher nogui %*\r\n"
	if normalizeNewlines(string(runBatData)) != normalizeNewlines(expectedRunBat) {
		return errors.New("run.bat does not match expected Fabric launch script")
	}

	shimPath := filepath.Join(serverDir, "fabric-server-launch.jar")
	zr, err := zip.OpenReader(shimPath)
	if err != nil {
		return fmt.Errorf("invalid fabric-server-launch.jar: %w", err)
	}
	defer zr.Close()

	manifestBytes, err := readZipEntry(&zr.Reader, "META-INF/MANIFEST.MF")
	if err != nil {
		return errors.New("missing META-INF/MANIFEST.MF in fabric-server-launch.jar")
	}
	expectedManifest := "Manifest-Version: 1.0\nMain-Class: net.fabricmc.loader.impl.launch.server.FabricServerLauncher\n" + wrapLongLine("Class-Path: "+strings.Join(classpath, " "), 70) + "\n"
	if normalizeNewlines(string(manifestBytes)) != normalizeNewlines(expectedManifest) {
		return errors.New("fabric-server-launch.jar manifest does not match expected FabricServerLauncher manifest")
	}

	propBytes, err := readZipEntry(&zr.Reader, "fabric-server-launch.properties")
	if err != nil {
		return errors.New("missing fabric-server-launch.properties in fabric-server-launch.jar")
	}
	if strings.TrimSpace(string(propBytes)) != "launch.mainClass=net.fabricmc.loader.impl.launch.knot.KnotServer" {
		return errors.New("fabric-server-launch.jar properties does not match KnotServer mainClass")
	}
	return nil
}

// verifyForgeLauncher checks the Forge launch scripts, the root shim when
// run.sh names it, and the launch arguments. A named root shim that fails
// verification is an error, never a silent omission.
func verifyForgeLauncher(serverDir string, locked document.LockedServer) (string, error) {
	shimRel := forgeRootShimPath(locked)
	argsRel := forgeArgsPath(locked.Product, locked.Minecraft.String(), locked.CoreVersion.String())
	rootShim, err := verifyForgeRunScript(serverDir, argsRel, shimRel)
	if err == nil && rootShim != "" {
		err = verifyForgeRootShim(serverDir, locked, rootShim)
	}
	if err == nil {
		err = verifyForgeLaunchFiles(serverDir, locked, argsRel)
	}
	return rootShim, err
}

// verifyForgeRunScript checks that run.sh is a generated Forge launcher: it runs
// the exact Forge launch command, with exec allowed, after blank and comment lines
// are removed. Only a script whose commands are exactly the root shim's Java check
// followed by the launch command names the root shim. Any other script that runs
// the launch command passes without naming the shim, so its extra commands never
// grant ownership. It returns the root shim, or "" when the script does not name it.
func verifyForgeRunScript(serverDir, argsRel, shimRel string) (string, error) {
	if err := checkPathSafety(serverDir, "run.sh", true); err != nil {
		return "", err
	}
	data, err := readBoundedFile(filepath.Join(serverDir, "run.sh"), 1<<20)
	if err != nil {
		return "", fmt.Errorf("reading run.sh: %w", err)
	}

	commands := forgeRunCommands(string(data))
	if n := len(commands); n > 0 {
		// The launch command is always last and may run under exec.
		commands[n-1] = strings.TrimPrefix(commands[n-1], "exec ")
	}
	launch := "java @user_jvm_args.txt @" + argsRel + " \"$@\""
	shimCheck := "java -jar " + shimRel + " --onlyCheckJava || exit 1"
	if shimRel != "" && slices.Equal(commands, []string{shimCheck, launch}) {
		return shimRel, nil
	}
	for _, command := range commands {
		if strings.TrimPrefix(command, "exec ") == launch {
			return "", nil
		}
	}
	return "", fmt.Errorf("run.sh does not run the generated Forge launch command (@user_jvm_args.txt @%s)", argsRel)
}

// forgeRunCommands returns the command lines of a launch script in order,
// without blank and comment lines.
func forgeRunCommands(script string) []string {
	var commands []string
	for line := range strings.SplitSeq(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			commands = append(commands, trimmed)
		}
	}
	return commands
}

// verifyForgeRootShim proves that the root shim named by run.sh is the Forge
// bootstrap shim the tree generated. The root copy must byte-match the
// canonical version-qualified shim under libraries, and that copy must open as
// a JAR whose manifest names the Forge shim main class. The canonical copy is
// not pinned by the lock, so this is semantic adoption evidence like generated
// outputs, not cryptographic proof.
func verifyForgeRootShim(serverDir string, locked document.LockedServer, shimRel string) error {
	canonicalRel := forgeVersionedPath(
		locked.Product,
		locked.Minecraft.String(),
		locked.CoreVersion.String(),
		shimRel,
	)
	rootDigest, err := regularFileSHA256(serverDir, shimRel)
	if err != nil {
		return fmt.Errorf("root shim %s: %w", shimRel, err)
	}
	canonicalDigest, err := regularFileSHA256(serverDir, canonicalRel)
	if err != nil {
		return fmt.Errorf("canonical shim %s: %w", canonicalRel, err)
	}
	if rootDigest != canonicalDigest {
		return fmt.Errorf("root shim %s does not match canonical shim %s", shimRel, canonicalRel)
	}
	return verifyForgeShimJar(serverDir, canonicalRel)
}

// verifyForgeShimJar accepts a regular ZIP whose manifest names the Forge shim
// main class. Other files, and JARs of other entry points, are rejected.
func verifyForgeShimJar(serverDir, rel string) error {
	zr, err := zip.OpenReader(filepath.Join(serverDir, filepath.FromSlash(rel)))
	if err != nil {
		return fmt.Errorf("canonical shim %s is not a JAR: %w", rel, err)
	}
	defer zr.Close()

	manifest, err := readZipEntry(&zr.Reader, "META-INF/MANIFEST.MF")
	if err != nil {
		return fmt.Errorf("canonical shim %s has no manifest: %w", rel, err)
	}
	if manifestMainClass(manifest) != forgeShimMainClass {
		return fmt.Errorf("canonical shim %s is not the Forge bootstrap shim", rel)
	}
	return nil
}

// manifestMainClass returns the Main-Class main attribute of a JAR manifest.
// Manifests may use CRLF line endings and wrap long attributes; the main class
// line itself is never wrapped.
func manifestMainClass(manifest []byte) string {
	for line := range strings.SplitSeq(string(manifest), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimRight(line, "\r"), "Main-Class:"); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// verifyForgeLaunchFiles checks the arguments file, run.bat, and
// user_jvm_args.txt that a Forge installer generates.
func verifyForgeLaunchFiles(serverDir string, locked document.LockedServer, argsRel string) error {
	if err := checkPathSafety(serverDir, "user_jvm_args.txt", true); err != nil {
		return err
	}
	if err := checkPathSafety(serverDir, argsRel, true); err != nil {
		return fmt.Errorf("launch arguments safety: %w", err)
	}
	stArgs, err := os.Lstat(filepath.Join(serverDir, filepath.FromSlash(argsRel)))
	if err != nil || !stArgs.Mode().IsRegular() {
		return fmt.Errorf("missing or irregular Forge launch arguments file %s", argsRel)
	}

	if err := checkPathSafety(serverDir, "run.bat", true); err != nil {
		return err
	}
	runBatPath := filepath.Join(serverDir, "run.bat")
	stBat, err := os.Lstat(runBatPath)
	if err != nil || !stBat.Mode().IsRegular() {
		return errors.New("missing or irregular run.bat")
	}
	runBatData, err := readBoundedFile(runBatPath, 1<<20)
	if err != nil {
		return fmt.Errorf("reading run.bat: %w", err)
	}
	winArgsRel := forgeWinArgsPath(locked.Product, locked.Minecraft.String(), locked.CoreVersion.String())
	if strings.Contains(string(runBatData), winArgsRel) {
		if err := checkPathSafety(serverDir, winArgsRel, true); err != nil {
			return fmt.Errorf("win_args.txt safety: %w", err)
		}
		stWinArgs, err := os.Lstat(filepath.Join(serverDir, filepath.FromSlash(winArgsRel)))
		if err != nil || !stWinArgs.Mode().IsRegular() {
			return fmt.Errorf("missing or irregular Forge launch arguments file %s", winArgsRel)
		}
	} else if !strings.Contains(string(runBatData), argsRel) {
		return fmt.Errorf("run.bat does not reference expected launch arguments (%s or %s)", winArgsRel, argsRel)
	}

	jvmPath := filepath.Join(serverDir, "user_jvm_args.txt")
	if st, err := os.Lstat(jvmPath); err != nil || !st.Mode().IsRegular() {
		return errors.New("missing or irregular user_jvm_args.txt")
	}
	return nil
}

// checkRegularFile verifies that rel names a regular file inside serverDir
// without following symlinks.
func checkRegularFile(serverDir, rel string) error {
	if err := checkPathSafety(serverDir, rel, true); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Join(serverDir, filepath.FromSlash(rel)))
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("missing or irregular %s", rel)
	}
	return nil
}

// regularFileSHA256 returns the SHA-256 of a regular file inside serverDir.
func regularFileSHA256(serverDir, rel string) (string, error) {
	if err := checkRegularFile(serverDir, rel); err != nil {
		return "", err
	}
	return fileSHA256(filepath.Join(serverDir, filepath.FromSlash(rel)))
}

// fabricLoaderPath returns the library path the Fabric adapter installs the
// locked loader to.
func fabricLoaderPath(version string) (string, error) {
	if version == "" || version == "." || version == ".." || strings.ContainsAny(version, "/\\") {
		return "", fmt.Errorf("invalid Fabric loader version %q", version)
	}
	p, err := maven.Path("net.fabricmc:fabric-loader:" + version)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join("libraries", p)), nil
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
