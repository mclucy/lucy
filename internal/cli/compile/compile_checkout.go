package compile

import (
	"context"
	"fmt"
	"io"
	"net/url"
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

func clone(ctx context.Context, remote, dir string, stderr io.Writer) error {
	git, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("find git: %w", err)
	}
	cmd := exec.CommandContext(ctx, git,
		"-c", "protocol.ext.allow=never",
		"clone", "--recurse-submodules", "--", remote, dir,
	)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	cmd.WaitDelay = 5 * time.Second
	configureProcess(cmd)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("clone repository: %w", err)
	}
	return nil
}
