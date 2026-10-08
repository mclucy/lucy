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

	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/upstream/routing"
)

func CopyLockedFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if err := out.Chmod(mode); err != nil {
		out.Close()
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func PrepareLocked(ctx context.Context, stage string, server lockfile.Server, corePath string, inputs map[string]string) error {
	if err := manifest.SafePath(server.Directory); err != nil {
		return err
	}
	root := filepath.Join(stage, server.Directory)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	var installer *zip.ReadCloser
	if server.Bootstrap == "forge-installer" {
		var err error
		installer, err = zip.OpenReader(corePath)
		if err != nil {
			return err
		}
		defer installer.Close()
	}
	for _, input := range server.Inputs {
		if err := manifest.SafePath(input.Path); err != nil {
			return err
		}
		dst := filepath.Join(root, filepath.FromSlash(input.Path))
		if input.Embedded != "" {
			if installer == nil {
				return fmt.Errorf("embedded input requires an installer")
			}
			if err := extractLockedEntry(&installer.Reader, input.Embedded, dst); err != nil {
				return err
			}
			continue
		}
		src, ok := inputs[input.Path]
		if !ok {
			return fmt.Errorf("missing locked bootstrap input %s", input.Path)
		}
		if err := CopyLockedFile(src, dst, 0o644); err != nil {
			return err
		}
	}
	switch server.Bootstrap {
	case "":
		return CopyLockedFile(corePath, filepath.Join(root, "server.jar"), 0o644)
	case "paperclip":
		if err := CopyLockedFile(corePath, filepath.Join(root, "server.jar"), 0o644); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, "java", "-Dpaperclip.patchonly=true", "-jar", "server.jar")
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Paperclip patching: %w\n%s", err, output)
		}
		return nil
	case "fabric":
		p, err := routing.MavenPath("net.fabricmc:fabric-loader:" + server.CoreVersion)
		if err != nil {
			return err
		}
		if err := CopyLockedFile(corePath, filepath.Join(root, "libraries", p), 0o644); err != nil {
			return err
		}
		classpath := []string{filepath.ToSlash(filepath.Join("libraries", p))}
		for _, input := range server.Inputs {
			if strings.HasPrefix(input.Path, "libraries/") {
				classpath = append(classpath, input.Path)
			}
		}
		unix := strings.Join(classpath, ":")
		windows := strings.Join(classpath, ";")
		if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\ncd -- \"$(dirname -- \"$0\")\"\nexec java -cp '"+unix+"' net.fabricmc.loader.impl.launch.server.FabricServerLauncher nogui \"$@\"\n"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, "run.bat"), []byte("@echo off\r\ncd /d %~dp0\r\njava -cp \""+windows+"\" net.fabricmc.loader.impl.launch.server.FabricServerLauncher nogui %*\r\n"), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(root, "fabric-server-launcher.properties"), []byte("serverJar=server.jar\n"), 0o644)
	case "forge-installer":
		return executeServerProfile(ctx, root, server, corePath, &installer.Reader)
	default:
		return fmt.Errorf("unsupported bootstrap adapter %q", server.Bootstrap)
	}
}

func extractLockedEntry(z *zip.Reader, name, dst string) error {
	for _, f := range z.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				return err
			}
			defer r.Close()
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			out, err := os.Create(dst)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, r)
			closeErr := out.Close()
			if err != nil {
				return err
			}
			return closeErr
		}
	}
	return fmt.Errorf("missing pinned installer entry %s", name)
}

func readLockedEntry(z *zip.Reader, name string) ([]byte, error) {
	for _, f := range z.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(io.LimitReader(r, 128<<20))
		}
	}
	return nil, fmt.Errorf("missing installer metadata %s", name)
}

func executeServerProfile(ctx context.Context, root string, server lockfile.Server, corePath string, z *zip.Reader) error {
	raw, err := readLockedEntry(z, "install_profile.json")
	if err != nil {
		return err
	}
	var profile struct {
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
	vars := map[string]string{"SIDE": "server", "ROOT": root, "LIBRARY_DIR": filepath.Join(root, "libraries"), "INSTALLER": corePath, "MINECRAFT_VERSION": server.Minecraft, "MINECRAFT_JAR": filepath.Join(root, "server.jar")}
	for key, value := range profile.Data {
		v := value.Server
		if strings.HasPrefix(v, "/") {
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
			p, err := routing.MavenPath(value[1 : len(value)-1])
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
				return fmt.Errorf("Mojang mappings processor has no output")
			}
			if err := CopyLockedFile(filepath.Join(root, "server-mappings.txt"), mappingOutput, 0o644); err != nil {
				return err
			}
			continue
		}
		p, err := routing.MavenPath(proc.Jar)
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
			p, err := routing.MavenPath(coordinate)
			if err != nil {
				return err
			}
			classpath = append(classpath, filepath.Join(root, "libraries", p))
		}
		commandArgs := []string{"-cp", strings.Join(classpath, string(os.PathListSeparator)), main}
		commandArgs = append(commandArgs, args...)
		cmd := exec.CommandContext(ctx, "java", commandArgs...)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("installer processor %s: %w\n%s", proc.Jar, err, output)
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
	if _, err := os.Stat(filepath.Join(root, "run.sh")); err != nil {
		prefix := "net/neoforged/neoforge/"
		if server.Distribution == "forge" {
			prefix = "net/minecraftforge/forge/"
		}
		version := server.CoreVersion
		if server.Distribution == "forge" {
			version = server.Minecraft + "-" + version
		}
		argsPath := "libraries/" + prefix + version + "/unix_args.txt"
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(argsPath))); err != nil {
			return fmt.Errorf("installer did not generate server launch arguments: %w", err)
		}
		if err := os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\ncd -- \"$(dirname -- \"$0\")\"\nexec java @user_jvm_args.txt @"+argsPath+" \"$@\"\n"), 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(root, "user_jvm_args.txt")); os.IsNotExist(err) {
			if err := os.WriteFile(filepath.Join(root, "user_jvm_args.txt"), nil, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
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
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
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
