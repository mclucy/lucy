package javainstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// systemVersionMajor returns the leading release number, because vendor
// repositories name a distribution by its major version.
func systemVersionMajor(version string) string {
	major, _, _ := strings.Cut(version, ".")
	return major
}

// systemRPMArch maps the Go architecture to the RPM base architecture.
func systemRPMArch(goarch string) string {
	switch goarch {
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
	policy Policy,
	host *Host,
	input io.Reader,
	out io.Writer,
) error {
	if repo == nil {
		return errors.New("no repository to add")
	}
	if repo.Key != nil {
		if err := addRepositoryKey(ctx, repo.Key, policy, host, input, out); err != nil {
			return err
		}
	}
	for _, file := range repo.Files {
		if err := writeRootFile(
			ctx,
			file.Path,
			[]byte(file.Content),
			policy,
			host,
			input,
			out,
		); err != nil {
			return fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	for _, step := range repo.Steps {
		if err := runStep(ctx, step, policy, host, input, out); err != nil {
			return err
		}
	}
	return nil
}
