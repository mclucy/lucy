package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/e2e/internal/testkit"
	"gopkg.in/yaml.v3"
)

type repoFixture struct {
	URL      string `yaml:"url"`
	Commit   string `yaml:"commit"`
	Platform string `yaml:"platform"`
	Project  string `yaml:"project"`
}

func runCompile(binary, repos string, ids []string, parallel int) int {
	if len(ids) == 0 {
		entries, err := os.ReadDir(repos)
		if err != nil {
			fmt.Fprintf(os.Stderr, "list compile fixtures: %v\n", err)
			return 1
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
				ids = append(ids, strings.TrimSuffix(entry.Name(), ".yaml"))
			}
		}
		if len(ids) == 0 {
			fmt.Fprintf(os.Stderr, "no compile fixtures in %s\n", repos)
			return 1
		}
	}
	return testkit.VerifyAll(ids, parallel, func(id string) error {
		return executeCompile(binary, repos, id)
	})
}

func executeCompile(binary, repos, id string) error {
	validID := filepath.Base(id) == id && id != "." && id != ".."
	if !validID {
		return fmt.Errorf("invalid fixture id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(repos, id+".yaml"))
	if err != nil {
		return fmt.Errorf("read compile fixture: %w", err)
	}
	var fixture repoFixture
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fixture); err != nil {
		return fmt.Errorf("decode compile fixture: %w", err)
	}
	if fixture.URL == "" {
		return fmt.Errorf("fixture requires a repository URL")
	}
	if len(fixture.Commit) != 40 || strings.Trim(fixture.Commit, "0123456789abcdef") != "" {
		return fmt.Errorf("fixture requires a full commit hash, got %q", fixture.Commit)
	}
	temporary, err := os.MkdirTemp("", "lucy-e2e-compile-")
	if err != nil {
		return fmt.Errorf("create compile sandbox: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(temporary); err != nil {
			fmt.Fprintf(os.Stderr, "remove compile sandbox: %v\n", err)
		}
	}()
	checkout := filepath.Join(temporary, "repository")
	if err := os.Mkdir(checkout, 0o755); err != nil {
		return fmt.Errorf("create checkout: %w", err)
	}
	environment := slices.DeleteFunc(os.Environ(), func(value string) bool {
		return strings.HasPrefix(value, "GIT_")
	})
	environment = append(environment, "GIT_TERMINAL_PROMPT=0")
	fmt.Printf("compile %s (%s @ %s)\n", id, fixture.URL, fixture.Commit)
	if err := fixtureGit(
		checkout,
		environment,
		"init",
		"--quiet",
		"--object-format=sha1",
	); err != nil {
		return err
	}
	if err := fixtureGit(
		checkout,
		environment,
		"remote",
		"add",
		"origin",
		fixture.URL,
	); err != nil {
		return err
	}
	if err := fixtureGit(
		checkout,
		environment,
		"fetch",
		"--depth=1",
		"origin",
		fixture.Commit,
	); err != nil {
		return err
	}
	if err := fixtureGit(
		checkout,
		environment,
		"checkout",
		"--detach",
		fixture.Commit,
	); err != nil {
		return err
	}
	if err := fixtureGit(
		checkout,
		environment,
		"submodule",
		"update",
		"--init",
		"--recursive",
	); err != nil {
		return err
	}
	// Lucy clones this detached HEAD, so revision selection belongs to the fixture runner.
	//
	// The checkout has to reach Lucy as a URL, and a URL path after the authority
	// always begins with a slash, so no spelling keeps a Windows drive letter in the
	// path. An empty host puts it in the host instead ("file://C:/...") and lucy
	// rejects that; naming localhost keeps it in the path as "/C:/...", which lucy
	// accepts and git resolves to the same directory.
	remote := (&url.URL{
		Scheme: "file",
		Host:   "localhost",
		Path:   filepath.ToSlash(checkout),
	}).String()
	args := []string{"compile", remote, "--output", filepath.Join(temporary, "artifact.jar"), "--no-style"}
	if fixture.Platform != "" {
		args = append(args, "--platform", fixture.Platform)
	}
	if fixture.Project != "" {
		args = append(args, "--project", fixture.Project)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = temporary
	cmd.Env = environment
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("lucy compile: %w", err)
	}
	return nil
}

func fixtureGit(dir string, environment []string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-c", "protocol.ext.allow=never"}, args...)...)
	cmd.Dir = dir
	cmd.Env = environment
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
