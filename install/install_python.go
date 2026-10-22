package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
	"gopkg.in/yaml.v3"
)

// pythonTarget names the platform whose wheels a lock pins. Python wheels are
// platform-specific, so a lock resolved on another operating system or
// architecture cannot be materialized locally.
func pythonTarget() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func prepareMCDR(ctx context.Context, root, directory string, selector version.Version, requirements []string, store *downloadStore) (*lockfile.MCDR, error) {
	if store.offline {
		return nil, fmt.Errorf("offline Python resolution needs a locked environment")
	}
	temp, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "python-resolve-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(temp)
	if captured, err := runProcess(exec.CommandContext(ctx, "python3", "-m", "venv", temp), store.trace.processOutput()); err != nil {
		return nil, fmt.Errorf("create Python environment: %w\n%s", err, captured)
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
		request += "==" + selector.String()
	}
	args := []string{
		"-m",
		"pip",
		"install",
		"--dry-run",
		"--ignore-installed",
		"--only-binary=:all:",
		"--report",
		reportPath,
		request,
	}
	for _, requirement := range requirements {
		if strings.HasPrefix(requirement, "-") {
			return nil, fmt.Errorf("invalid Python requirement %q", requirement)
		}
		args = append(args, requirement)
	}
	if captured, err := runProcess(exec.CommandContext(ctx, python, args...), store.trace.processOutput()); err != nil {
		return nil, fmt.Errorf("resolve MCDR Python inputs: %w\n%s", err, captured)
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
	out := &lockfile.MCDR{
		Directory: directory,
		Python:    strings.TrimSpace(string(versionBytes)),
		Target:    pythonTarget(),
		Inputs:    []server.BootstrapInput{},
	}
	for _, pkg := range report.Install {
		filename := filepath.Base(strings.Split(pkg.Download.URL, "?")[0])
		a := catalog.Artifact{
			Provider:  catalog.ProviderPyPI,
			ProjectID: pkg.Metadata.Name,
			Version:   version.Version(pkg.Metadata.Version),
			Filename:  filename,
			URL:       pkg.Download.URL,
			Hashes:    pkg.Download.Archive.Hashes,
		}
		materialized, err := store.materialize(&a)
		if err != nil {
			return nil, err
		}
		a.Hashes = materialized.Hashes
		out.Inputs = append(out.Inputs, server.BootstrapInput{
			Path:     "wheels/" + filename,
			Artifact: a,
		})
		if strings.EqualFold(pkg.Metadata.Name, "mcdreforged") {
			out.Version = version.Version(pkg.Metadata.Version)
		}
	}
	if out.Version == "" {
		return nil, fmt.Errorf("python resolution omitted mcdreforged")
	}
	return out, nil
}

// runProcess streams a subprocess's combined output to output while retaining
// it for the error message.
func runProcess(cmd *exec.Cmd, output io.Writer) ([]byte, error) {
	if output == nil {
		output = io.Discard
	}
	var captured bytes.Buffer
	writer := io.MultiWriter(output, &captured)
	cmd.Stdout = writer
	cmd.Stderr = writer
	err := cmd.Run()
	return captured.Bytes(), err
}

func preparePython(ctx context.Context, root, stage string, d *lockfile.Document, store *downloadStore) error {
	m := d.MCDR
	if m.Target != pythonTarget() {
		return fmt.Errorf("locked %s environment targets %s, current target is %s", server.EnvironmentMCDR, m.Target, pythonTarget())
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
		if captured, err := runProcess(exec.CommandContext(ctx, "python3", "-m", "venv", env), store.trace.processOutput()); err != nil {
			return fmt.Errorf("create locked MCDR environment: %w\n%s", err, captured)
		}
		var requirements []string
		for i := range m.Inputs {
			input := &m.Inputs[i]
			materialized, err := store.materialize(&input.Artifact)
			if err != nil {
				return err
			}
			input.Artifact.Hashes = materialized.Hashes
			requirements = append(requirements, materialized.Path+" --hash=sha256:"+materialized.Hashes["sha256"])
		}
		requirementsPath := filepath.Join(env, "locked.txt")
		if err := os.WriteFile(requirementsPath, []byte(strings.Join(requirements, "\n")+"\n"), 0o600); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, python, "-m", "pip", "install", "--no-index", "--no-deps", "--require-hashes", "-r", requirementsPath)
		if captured, err := runProcess(cmd, store.trace.processOutput()); err != nil {
			os.RemoveAll(env)
			return fmt.Errorf("materialize locked MCDR environment: %w\n%s", err, captured)
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
	if d.Server.Bootstrapper == "fabric" || d.Server.Bootstrapper == "forge-installer" {
		entry = "sh run.sh"
	}
	relative, err := filepath.Rel(m.Directory, d.Server.Directory)
	if err != nil {
		return err
	}
	// MCDReforged parses the server log stream through a handler that matches
	// the launcher, so a Bukkit-family server needs its own parser to report
	// start-up and player events at all.
	launchHandler := "vanilla_handler"
	switch d.Server.Bootstrapper {
	case "forge-installer":
		launchHandler = "forge_handler"
	case "paperclip":
		launchHandler = "bukkit_handler"
	}
	config := map[string]any{
		"working_directory": filepath.ToSlash(relative),
		"start_command":     entry,
		"handler":           launchHandler,
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
