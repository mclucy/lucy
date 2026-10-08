package compile

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var githubReference = regexp.MustCompile(`^[A-Za-z0-9_-]+/[A-Za-z0-9_.-]+$`)

func parseRemote(source string) (string, error) {
	if githubReference.MatchString(source) {
		owner, repository, _ := strings.Cut(source, "/")
		if repository == "." || repository == ".." {
			return "", fmt.Errorf("invalid repository reference %q", source)
		}
		return "https://github.com/" + owner + "/" + strings.TrimSuffix(repository, ".git") + ".git", nil
	}
	remote, err := url.Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse repository url: %w", err)
	}
	if remote.RawQuery != "" || remote.Fragment != "" || remote.Opaque != "" {
		return "", fmt.Errorf("repository url must not contain a query, fragment, or opaque transport")
	}
	switch remote.Scheme {
	case "https", "http", "ssh", "git":
		if remote.Host == "" || strings.Trim(remote.Path, "/") == "" {
			return "", fmt.Errorf("repository url must identify a host and repository path")
		}
	case "file":
		if remote.Host != "" && remote.Host != "localhost" {
			return "", fmt.Errorf("file repository url must refer to the local host")
		}
		if !filepath.IsAbs(remote.Path) {
			return "", fmt.Errorf("file repository url must have an absolute path")
		}
	default:
		return "", fmt.Errorf("source must be a Git repository url or GitHub owner/repo reference")
	}
	return remote.String(), nil
}

// clone fetches remote into dir. Its raw output goes to output rather than
// straight to the terminal, so a large checkout stays quiet and a rejected
// remote is reported with git's own reason.
func clone(ctx context.Context, remote, dir, branch, tag string, output *processLog) error {
	git, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("find git: %w", err)
	}
	args := []string{"-c", "protocol.ext.allow=never", "clone", "--recurse-submodules"}
	switch {
	case branch != "":
		args = append(args, "--branch", branch)
	case tag != "":
		args = append(args, "--branch", tag)
	}
	args = append(args, "--", remote, dir)
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Env = cloneEnvironment()
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.WaitDelay = 5 * time.Second
	configureProcess(cmd)
	err = cmd.Run()
	output.flush()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return reportFailure(output, fmt.Errorf("clone repository: %w", err))
	}
	return nil
}

// cloneEnvironment makes the clone non-interactive. The command runs in its own
// process group, so a prompt waiting on the terminal is stopped by the kernel
// (SIGTTIN) with no way to answer it and the command hangs until Ctrl+C;
// prompts instead fail fast. HTTPS credentials honor GIT_TERMINAL_PROMPT,
// host-key and passphrase prompts honor batch-mode ssh, and a user who
// configured their own GIT_SSH_COMMAND or GIT_SSH keeps it.
func cloneEnvironment() []string {
	env := replaceEnvironment(os.Environ(), "GIT_TERMINAL_PROMPT", "0")
	if _, configured := os.LookupEnv("GIT_SSH_COMMAND"); !configured {
		if _, configured := os.LookupEnv("GIT_SSH"); !configured {
			env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
		}
	}
	return env
}
