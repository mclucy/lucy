package bootstrap

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/maven"
	"github.com/mclucy/lucy/internal/workspacewrite"
)

// prepareForgeInstaller runs the processors of a Forge or NeoForge installer
// profile and materializes the server launch scripts they generate.
func prepareForgeInstaller(ctx context.Context, root string, server document.LockedServer, corePath string, z *zip.Reader, java string, output io.Writer) error {
	raw, err := readLockedEntry(z, "install_profile.json")
	if err != nil {
		return err
	}
	var profile struct {
		// Path names the launch artifact the installer extracts from its
		// bundled Maven tree. Shim-era Forge profiles name the root shim.
		Path string `json:"path"`
		Data map[string]struct {
			Server string `json:"server"`
		} `json:"data"`
		Processors []struct {
			Sides     []string          `json:"sides"`
			Jar       string            `json:"jar"`
			Classpath []string          `json:"classpath"`
			Args      []string          `json:"args"`
			Outputs   map[string]string `json:"outputs"`
		} `json:"processors"`
	}
	if err := json.Unmarshal(raw, &profile); err != nil {
		return err
	}
	if profile.Path != "" {
		if err := extractProfileArtifact(root, z, profile.Path); err != nil {
			return err
		}
	}
	vars := map[string]string{
		"SIDE":              "server",
		"ROOT":              root,
		"LIBRARY_DIR":       filepath.Join(root, "libraries"),
		"INSTALLER":         corePath,
		"MINECRAFT_VERSION": server.Minecraft.String(),
		"MINECRAFT_JAR":     filepath.Join(root, "server.jar"),
	}
	for key, value := range profile.Data {
		v := value.Server
		if strings.HasPrefix(v, "/") {
			if key == "" || key == "." || key == ".." || strings.ContainsAny(key, "/\\") {
				return fmt.Errorf("invalid installer data key %q", key)
			}
			destination := filepath.Join(root, ".bootstrap-data", key)
			if err := extractLockedEntry(z, strings.TrimPrefix(v, "/"), destination); err != nil {
				return err
			}
			vars[key] = destination
		} else {
			vars[key] = v
		}
	}
	var expand func(string) (string, error)
	expand = func(value string) (string, error) {
		if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
			return strings.Trim(value, "'"), nil
		}
		if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
			p, err := safeForgeMavenPath(value[1 : len(value)-1])
			return filepath.Join(root, "libraries", p), err
		}
		for range 16 {
			start := strings.IndexByte(value, '{')
			if start < 0 {
				return value, nil
			}
			end := strings.IndexByte(value[start:], '}')
			if end < 0 {
				return "", fmt.Errorf("invalid processor token %q", value)
			}
			end += start
			key := value[start+1 : end]
			replacement, ok := vars[key]
			if !ok {
				return "", fmt.Errorf("unknown processor variable %s", key)
			}
			if strings.HasPrefix(replacement, "[") {
				var err error
				replacement, err = expand(replacement)
				if err != nil {
					return "", err
				}
			} else {
				replacement = strings.Trim(replacement, "'")
			}
			value = value[:start] + replacement + value[end+1:]
		}
		return "", fmt.Errorf("recursive installer variable expansion")
	}
	for _, proc := range profile.Processors {
		if len(proc.Sides) > 0 {
			serverSide := false
			for _, side := range proc.Sides {
				serverSide = serverSide || side == "server"
			}
			if !serverSide {
				continue
			}
		}
		args := make([]string, len(proc.Args))
		for i, arg := range proc.Args {
			args[i], err = expand(arg)
			if err != nil {
				return err
			}
		}
		mappingTask := false
		var mappingOutput string
		for i, arg := range args {
			if arg == "DOWNLOAD_MOJMAPS" {
				mappingTask = true
			}
			if arg == "--output" && i+1 < len(args) {
				mappingOutput = args[i+1]
			}
		}
		if mappingTask {
			if mappingOutput == "" {
				return fmt.Errorf("mojang mappings processor has no output")
			}
			relative, err := filepath.Rel(root, mappingOutput)
			if err != nil || !filepath.IsLocal(relative) {
				return fmt.Errorf("installer mapping output %q escapes staging", mappingOutput)
			}
			if err := workspacewrite.SafePath(root, relative); err != nil {
				return err
			}
			if err := fsutil.AtomicCopyFile(filepath.Join(root, "server-mappings.txt"), mappingOutput, 0o644); err != nil {
				return err
			}
			continue
		}
		p, err := safeForgeMavenPath(proc.Jar)
		if err != nil {
			return err
		}
		processorJar := filepath.Join(root, "libraries", p)
		main, err := jarMain(processorJar)
		if err != nil {
			return err
		}
		classpath := []string{processorJar}
		for _, coordinate := range proc.Classpath {
			p, err := safeForgeMavenPath(coordinate)
			if err != nil {
				return err
			}
			classpath = append(classpath, filepath.Join(root, "libraries", p))
		}
		commandArgs := []string{
			"-cp",
			strings.Join(classpath, string(os.PathListSeparator)),
			main,
		}
		commandArgs = append(commandArgs, args...)
		if err := requireJava(java, "installer processor "+proc.Jar); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, java, commandArgs...)
		cmd.Dir = root
		captured, err := runLockedProcess(cmd, output)
		if err != nil {
			return fmt.Errorf("installer processor %s: %w\n%s", proc.Jar, err, captured)
		}
		for name, expected := range proc.Outputs {
			destination, err := expand(name)
			if err != nil {
				return err
			}
			expected, err = expand(expected)
			if err != nil {
				return err
			}
			if err := checkProcessorOutput(destination, expected); err != nil {
				return err
			}
		}
	}
	// Profile extraction processors generate the native server launch scripts.
	argsPath := forgeArgsPath(server.Product, server.Minecraft.String(), server.CoreVersion.String())
	if _, err := os.Stat(filepath.Join(root, "run.sh")); err != nil {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(argsPath))); err != nil {
			return fmt.Errorf("installer did not generate server launch arguments: %w", err)
		}
		if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\ncd -- \"$(dirname -- \"$0\")\"\nexec java @user_jvm_args.txt @"+argsPath+" \"$@\"\n"), 0o755); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(root, "run.bat")); os.IsNotExist(err) {
		winArgsPath := forgeWinArgsPath(server.Product, server.Minecraft.String(), server.CoreVersion.String())
		argsForBat := winArgsPath
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(winArgsPath))); err != nil {
			argsForBat = argsPath
		}
		if err := os.WriteFile(filepath.Join(root, "run.bat"), []byte("@echo off\r\ncd /d %~dp0\r\njava @user_jvm_args.txt @"+argsForBat+" %*\r\n"), 0o644); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(root, "user_jvm_args.txt")); os.IsNotExist(err) {
		if err := os.WriteFile(filepath.Join(root, "user_jvm_args.txt"), nil, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// extractProfileArtifact writes the launch artifact an installer profile
// names. The installer bundles it under maven/, mirroring the repository path
// it would otherwise download, and extracts it to the server root under its
// filename. Shim-era Forge profiles name the root shim here.
func extractProfileArtifact(root string, z *zip.Reader, coordinate string) error {
	rel, err := maven.Path(coordinate)
	if err != nil {
		return fmt.Errorf("installer profile artifact: %w", err)
	}
	if err := document.SafeRelativePath(rel); err != nil || !filepath.IsLocal(rel) {
		return fmt.Errorf("invalid installer profile artifact %q", coordinate)
	}
	return extractLockedEntry(
		z,
		"maven/"+rel,
		filepath.Join(root, filepath.Base(rel)),
	)
}

func safeForgeMavenPath(coordinate string) (string, error) {
	p, err := maven.Path(coordinate)
	if err != nil {
		return "", err
	}
	if err := document.SafeRelativePath(p); err != nil || !filepath.IsLocal(p) {
		return "", fmt.Errorf("invalid installer coordinate %q", coordinate)
	}
	return p, nil
}

func readLockedEntry(z *zip.Reader, name string) ([]byte, error) {
	raw, found, err := fsutil.ReadZipEntry(
		z,
		name,
		fsutil.MaxNestedZipEntryBytes,
	)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("missing installer metadata %s", name)
	}
	return raw, nil
}

func jarMain(p string) (string, error) {
	z, err := zip.OpenReader(p)
	if err != nil {
		return "", err
	}
	defer z.Close()
	raw, err := readLockedEntry(&z.Reader, "META-INF/MANIFEST.MF")
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if v, ok := strings.CutPrefix(line, "Main-Class: "); ok {
			return strings.TrimSpace(v), nil
		}
	}
	return "", fmt.Errorf("processor JAR has no Main-Class")
}

func checkProcessorOutput(filename, expected string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if actual := hex.EncodeToString(h.Sum(nil)); actual != expected {
		return fmt.Errorf("processor output %s: expected sha1 %s, got %s", filename, expected, actual)
	}
	return nil
}
