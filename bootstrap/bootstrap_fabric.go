package bootstrap

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
)

// prepareFabric places the Fabric loader on the classpath and generates the
// launch scripts and shim JAR that start it.
func prepareFabric(root string, server document.LockedServer, corePath string) error {
	loaderPath, err := fabricLoaderPath(server.CoreVersion.String())
	if err != nil {
		return err
	}
	if err := fsutil.AtomicCopyFile(corePath, filepath.Join(root, filepath.FromSlash(loaderPath)), 0o644); err != nil {
		return err
	}
	classpath := loaderClasspath(loaderPath, server.Inputs)
	unix := strings.Join(classpath, ":")
	windows := strings.Join(classpath, ";")
	if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\ncd -- \"$(dirname -- \"$0\")\"\nexec java -cp '"+unix+"' "+fabricLauncherMainClass+" nogui \"$@\"\n"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "run.bat"), []byte("@echo off\r\ncd /d %~dp0\r\njava -cp \""+windows+"\" "+fabricLauncherMainClass+" nogui %*\r\n"), 0o644); err != nil {
		return err
	}
	if err := writeFabricShimJar(filepath.Join(root, "fabric-server-launch.jar"), classpath); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "fabric-server-launcher.properties"), []byte("serverJar=server.jar\n"), 0o644); err != nil {
		return err
	}
	return nil
}

const (
	// fabricLauncherMainClass is the entry point of the Fabric launcher JAR.
	fabricLauncherMainClass = "net.fabricmc.loader.impl.launch.server.FabricServerLauncher"
	// fabricKnotMainClass is the server main class the launcher starts.
	fabricKnotMainClass = "net.fabricmc.loader.impl.launch.knot.KnotServer"
)

func writeFabricShimJar(path string, classpath []string) error {
	return writeLauncherShimJar(
		path,
		"fabric-server-launch.properties",
		fabricLauncherMainClass,
		fabricKnotMainClass,
		classpath,
	)
}

// writeLauncherShimJar writes the manifest-only launcher JAR shared by the
// loader adapters. The embedded properties entry selects the server main class.
func writeLauncherShimJar(path, propertiesEntry, mainClass, launchMainClass string, classpath []string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	defer writer.Close()

	manifest := "Manifest-Version: 1.0\nMain-Class: " + mainClass + "\n" +
		wrapLongLine("Class-Path: "+strings.Join(classpath, " "), 70) + "\n"
	manifestEntry, err := writer.Create("META-INF/MANIFEST.MF")
	if err != nil {
		return err
	}
	if _, err := manifestEntry.Write([]byte(manifest)); err != nil {
		return err
	}

	propertiesEntryWriter, err := writer.Create(propertiesEntry)
	if err != nil {
		return err
	}
	_, err = propertiesEntryWriter.Write([]byte("launch.mainClass=" + launchMainClass + "\n"))
	return err
}

func wrapLongLine(line string, width int) string {
	if len(line) <= width {
		return line
	}
	var builder strings.Builder
	for len(line) > width {
		builder.WriteString(line[:width])
		builder.WriteByte('\n')
		builder.WriteByte(' ')
		line = line[width:]
	}
	builder.WriteString(line)
	return builder.String()
}
