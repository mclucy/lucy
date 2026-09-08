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
		if !isAbsoluteFilePath(remote.Path) {
			return "", fmt.Errorf("file repository url must have an absolute path")
		}
	default:
		return "", fmt.Errorf("source must be a Git repository url or GitHub owner/repo reference")
	}
	return remote.String(), nil
}

// isAbsoluteFilePath reports whether a file URL path is absolute. Windows paths
// carry their drive letter after a leading slash inside the URL, and no native
// absolute-path check accepts that spelling on Windows, so recognize it directly.
// The URL is handed to git unchanged either way, only its validity is judged here.
func isAbsoluteFilePath(path string) bool {
	if len(path) >= 3 && path[0] == '/' && isDriveLetter(path[1]) && path[2] == ':' {
		return true
	}
	return filepath.IsAbs(path)
}

func isDriveLetter(c byte) bool {
	return ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z')
}

// clone fetches remote into dir. Its raw output goes to output rather than
// straight to the terminal, so a large checkout stays quiet and a rejected
// remote is reported with git's own reason.
func clone(ctx context.Context, remote, dir, branch, tag string, output *processLog) error {
	git, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("find git: %w", err)
	}
	env := cloneEnvironment()
	// run labels one git invocation; a failure quotes the label along with the
	// tail of git's own output.
	run := func(label string, args []string) error {
		cmd := exec.CommandContext(ctx, git, args...)
		cmd.Env = env
		cmd.Stdout = output
		cmd.Stderr = output
		cmd.WaitDelay = 5 * time.Second
		configureProcess(cmd)
		err := cmd.Run()
		output.flush()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return reportFailure(output, fmt.Errorf("%s: %w", label, err))
		}
		return nil
	}

	// git resolves --branch under refs/heads before refs/tags, so a tag named
	// like a branch would clone the branch. When a ref is requested, clone
	// without a checkout, resolve the exact namespace, and only then check
	// out that commit and initialize the submodules.
	args := []string{"-c", "protocol.ext.allow=never", "clone"}
	if branch == "" && tag == "" {
		args = append(args, "--recurse-submodules")
	} else {
		args = append(args, "--no-checkout")
	}
	args = append(args, "--", remote, dir)
	if err := run("clone repository", args); err != nil {
		return err
	}
	if branch == "" && tag == "" {
		return nil
	}
	// A clone keeps the fetched branches under the origin remote and copies the
	// tags verbatim, so branches resolve through refs/remotes/origin and tags
	// through refs/tags.
	kind, namespace := "branch", "refs/remotes/origin/"
	if branch == "" {
		kind, namespace = "tag", "refs/tags/"
	}
	name := branch
	if name == "" {
		name = tag
	}
	ref := namespace + name
	if err := run(fmt.Sprintf("remote has no %s %q", kind, name),
		[]string{"-c", "protocol.ext.allow=never", "-C", dir, "show-ref", "--verify", "--quiet", ref},
	); err != nil {
		return err
	}
	if err := run(fmt.Sprintf("checkout %s %s", kind, name),
		[]string{"-c", "protocol.ext.allow=never", "-C", dir, "checkout", "--detach", ref},
	); err != nil {
		return err
	}
	return run("initialize submodules",
		[]string{"-c", "protocol.ext.allow=never", "-C", dir, "submodule", "update", "--init", "--recursive"},
	)
}

// cloneEnvironment makes every git invocation of the clone non-interactive. The
// commands run in their own process group, so a prompt waiting on the terminal
// is stopped by the kernel (SIGTTIN) with no way to answer it and the command
// hangs until Ctrl+C; prompts instead fail fast. HTTPS credentials honor
// GIT_TERMINAL_PROMPT, host-key and passphrase prompts honor batch-mode ssh,
// and a user who configured their own GIT_SSH_COMMAND or GIT_SSH keeps it.
func cloneEnvironment() []string {
	env := replaceEnvironment(os.Environ(), "GIT_TERMINAL_PROMPT", "0")
	if _, configured := os.LookupEnv("GIT_SSH_COMMAND"); !configured {
		if _, configured := os.LookupEnv("GIT_SSH"); !configured {
			env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
		}
	}
	return env
}
