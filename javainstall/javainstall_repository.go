package javainstall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/log"
)

// probeTimeout bounds a read-only package manager query. A query that exceeds
// it counts as a missing package.
const probeTimeout = 30 * time.Second

// systemToolAvailable reports whether every named binary is on PATH. System
// package managers run only on Linux.
func systemToolAvailable(names ...string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			return false
		}
	}
	return true
}

// systemProbe runs a read-only query with a bounded wait. The error carries
// stderr so the caller can log why the query failed.
func systemProbe(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("empty system probe")
	}
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	command := exec.CommandContext(probeCtx, args[0], args[1:]...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			return stdout.Bytes(), fmt.Errorf(
				"%s: %w",
				strings.Join(args, " "),
				err,
			)
		}
		return stdout.Bytes(), fmt.Errorf(
			"%s: %w: %s",
			strings.Join(args, " "),
			err,
			detail,
		)
	}
	return stdout.Bytes(), nil
}

var systemReleaseOnce = sync.OnceValue(readSystemRelease)

// systemRelease returns the fields of /etc/os-release. An absent file yields
// an empty map.
func systemRelease() map[string]string {
	return systemReleaseOnce()
}

func readSystemRelease() map[string]string {
	fields := map[string]string{}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		log.Debug("read os-release failed", "reason", err)
		return fields
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		fields[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return fields
}

// systemVersionMajor returns the leading release number, because vendor
// repositories name a distribution by its major version.
func systemVersionMajor(version string) string {
	major, _, _ := strings.Cut(version, ".")
	return major
}

// systemRPMArch maps the Go architecture to the RPM base architecture.
func systemRPMArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "386":
		return "i686"
	case "arm64":
		return "aarch64"
	case "ppc64le":
		return "ppc64le"
	case "s390x":
		return "s390x"
	default:
		return ""
	}
}

// addRepository writes the verified key and the source files, then runs the
// repository steps. Every write happens as root.
func addRepository(
	ctx context.Context,
	repo *Repository,
	host Host,
	out io.Writer,
) error {
	if repo == nil {
		return errors.New("no repository to add")
	}
	if repo.Key != nil {
		if err := addRepositoryKey(ctx, repo.Key, host, out); err != nil {
			return err
		}
	}
	for _, file := range repo.Files {
		if err := writeRootFile(
			ctx,
			file.Path,
			[]byte(file.Content),
			host,
			out,
		); err != nil {
			return fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	for _, step := range repo.Steps {
		if err := runStep(ctx, step, host, out); err != nil {
			return err
		}
	}
	return nil
}
