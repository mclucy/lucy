package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

func resolvePython(ctx context.Context, root, directory, selector string, requirements []string, store *artifactStore) (*lockfile.MCDR, error) {
	if store.offline {
		return nil, fmt.Errorf("offline Python resolution needs a locked environment")
	}
	temp, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "python-resolve-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temp)
	if output, err := exec.CommandContext(ctx, "python3", "-m", "venv", temp).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("create Python environment: %w\n%s", err, output)
	}
	python := filepath.Join(temp, "bin", "python")
	if runtime.GOOS == "windows" {
		python = filepath.Join(temp, "Scripts", "python.exe")
	}
	versionBytes, err := exec.CommandContext(ctx, python, "-c", "import sys;print(str(sys.version_info.major)+'.'+str(sys.version_info.minor))").Output()
	if err != nil {
		return nil, err
	}
	reportPath := filepath.Join(temp, "report.json")
	request := "mcdreforged"
	if !isFloating(selector) {
		request += "==" + selector
	}
	args := []string{"-m", "pip", "install", "--dry-run", "--ignore-installed", "--only-binary=:all:", "--report", reportPath, request}
	for _, requirement := range requirements {
		if strings.HasPrefix(requirement, "-") {
			return nil, fmt.Errorf("invalid Python requirement %q", requirement)
		}
		args = append(args, requirement)
	}
	if output, err := exec.CommandContext(ctx, python, args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("resolve MCDR Python inputs: %w\n%s", err, output)
	}
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, err
	}
	var report struct {
		Install []struct {
			Download struct {
				URL     string `json:"url"`
				Archive struct {
					Hashes map[string]string `json:"hashes"`
				} `json:"archive_info"`
			} `json:"download_info"`
			Metadata struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"metadata"`
		} `json:"install"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, err
	}
	out := &lockfile.MCDR{Directory: directory, Python: strings.TrimSpace(string(versionBytes)), Platform: runtime.GOOS + "/" + runtime.GOARCH, Inputs: []types.BootstrapInput{}}
	for _, pkg := range report.Install {
		filename := filepath.Base(strings.Split(pkg.Download.URL, "?")[0])
		a := types.Artifact{Provider: "pypi", ProjectID: pkg.Metadata.Name, Version: pkg.Metadata.Version, Filename: filename, URL: pkg.Download.URL, Hashes: pkg.Download.Archive.Hashes}
		if _, err := store.acquire(&a); err != nil {
			return nil, err
		}
		out.Inputs = append(out.Inputs, types.BootstrapInput{Path: "wheels/" + filename, Artifact: a})
		if strings.EqualFold(pkg.Metadata.Name, "mcdreforged") {
			out.Version = pkg.Metadata.Version
		}
	}
	if out.Version == "" {
		return nil, fmt.Errorf("Python resolution omitted mcdreforged")
	}
	return out, nil
}

func preparePython(ctx context.Context, root, stage string, d *lockfile.Document, store *artifactStore) error {
	m := d.MCDR
	if m.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return fmt.Errorf("locked MCDR environment targets %s, current target is %s/%s", m.Platform, runtime.GOOS, runtime.GOARCH)
	}
	pyVersion, err := exec.CommandContext(ctx, "python3", "-c", "import sys;print(str(sys.version_info.major)+'.'+str(sys.version_info.minor))").Output()
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(pyVersion)) != m.Python {
		return fmt.Errorf("locked MCDR requires Python %s", m.Python)
	}
	encoded, err := json.Marshal(m, json.Deterministic(true))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(encoded)
	env := filepath.Join(root, ".lucy", "python", hex.EncodeToString(sum[:]))
	python := filepath.Join(env, "bin", "python")
	if runtime.GOOS == "windows" {
		python = filepath.Join(env, "Scripts", "python.exe")
	}
	if _, err := os.Stat(filepath.Join(env, ".complete")); os.IsNotExist(err) {
		if output, err := exec.CommandContext(ctx, "python3", "-m", "venv", env).CombinedOutput(); err != nil {
			return fmt.Errorf("create locked MCDR environment: %w\n%s", err, output)
		}
		requirements := []string{}
		for i := range m.Inputs {
			input := &m.Inputs[i]
			p, err := store.acquire(&input.Artifact)
			if err != nil {
				return err
			}
			requirements = append(requirements, p+" --hash=sha256:"+input.Hashes["sha256"])
		}
		requirementsPath := filepath.Join(env, "locked.txt")
		if err := os.WriteFile(requirementsPath, []byte(strings.Join(requirements, "\n")+"\n"), 0o600); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, python, "-m", "pip", "install", "--no-index", "--no-deps", "--require-hashes", "-r", requirementsPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			os.RemoveAll(env)
			return fmt.Errorf("materialize locked MCDR environment: %w\n%s", err, output)
		}
		if err := os.WriteFile(filepath.Join(env, ".complete"), nil, 0o600); err != nil {
			return err
		}
	}
	wrapper := filepath.Join(stage, m.Directory)
	if err := os.MkdirAll(wrapper, 0o755); err != nil {
		return err
	}
	entry := "java -jar server.jar nogui"
	if d.Server.Bootstrap == "fabric" || d.Server.Bootstrap == "forge-installer" {
		entry = "sh run.sh"
	}
	relative, err := filepath.Rel(m.Directory, d.Server.Directory)
	if err != nil {
		return err
	}
	config := map[string]any{"working_directory": filepath.ToSlash(relative), "start_command": entry, "handler": "vanilla_handler"}
	if d.Server.Distribution == "forge" || d.Server.Distribution == "neoforge" {
		config["handler"] = "forge_handler"
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(wrapper, "config.yml"), data, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(wrapper, "run-mcdr.sh"), []byte("#!/bin/sh\ncd -- \"$(dirname -- \"$0\")\"\nexec '"+python+"' -m mcdreforged \"$@\"\n"), 0o755); err != nil {
		return err
	}
	return nil
}
