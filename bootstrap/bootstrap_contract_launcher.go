package bootstrap

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/maven"
	serverpkg "github.com/mclucy/lucy/server"
)

// verifyLauncherSemantics checks method-specific launch script and shim JAR semantics.
// This single verification path is shared across post-stage validation and in-place adoption.
func verifyLauncherSemantics(serverDir string, locked document.LockedServer) error {
	switch locked.Bootstrapper {
	case serverpkg.BootstrapMethodPlain, serverpkg.BootstrapMethodPaperclip:
		if err := checkPathSafety(serverDir, "server.jar", true); err != nil {
			return err
		}
		st, err := os.Lstat(filepath.Join(serverDir, "server.jar"))
		if err != nil {
			return errors.New("missing server.jar")
		}
		if !st.Mode().IsRegular() {
			return errors.New("server.jar is not a regular file")
		}
		return nil

	case serverpkg.BootstrapMethodFabric:
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

	case serverpkg.BootstrapMethodForgeInstaller:
		if err := checkPathSafety(serverDir, "run.sh", true); err != nil {
			return err
		}
		if err := checkPathSafety(serverDir, "user_jvm_args.txt", true); err != nil {
			return err
		}

		argsRel := forgeArgsPath(locked.Product, locked.Minecraft.String(), locked.CoreVersion.String())
		if err := checkPathSafety(serverDir, argsRel, true); err != nil {
			return fmt.Errorf("launch arguments safety: %w", err)
		}
		stArgs, err := os.Lstat(filepath.Join(serverDir, filepath.FromSlash(argsRel)))
		if err != nil || !stArgs.Mode().IsRegular() {
			return fmt.Errorf("missing or irregular Forge launch arguments file %s", argsRel)
		}

		runShPath := filepath.Join(serverDir, "run.sh")
		runShData, err := readBoundedFile(runShPath, 1<<20)
		if err != nil {
			return fmt.Errorf("reading run.sh: %w", err)
		}
		expectedExec := "java @user_jvm_args.txt @" + argsRel + " \"$@\""
		hasExpectedExec := false
		for _, line := range strings.Split(string(runShData), "\n") {
			trimmed := strings.TrimSpace(line)
			trimmed = strings.TrimPrefix(trimmed, "exec ")
			if trimmed == expectedExec {
				hasExpectedExec = true
				break
			}
		}
		if !hasExpectedExec {
			return fmt.Errorf("run.sh does not launch Forge with expected arguments (@user_jvm_args.txt @%s)", argsRel)
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

	default:
		return nil
	}
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

// runScriptReferences reports whether run.sh exists and names rel. The
// generated launcher is the evidence that a root shim belongs to the tree.
func runScriptReferences(serverDir, rel string) bool {
	data, err := readBoundedFile(filepath.Join(serverDir, "run.sh"), 1<<20)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), filepath.Base(rel))
}

// isForgeDiscardInput reports whether a locked Forge input exists only for
// installer processor execution.
func isForgeDiscardInput(cleanPath string) bool {
	return cleanPath == "server-mappings.txt" ||
		cleanPath == "server.jar" ||
		strings.HasPrefix(cleanPath, ".bootstrap-data/")
}
