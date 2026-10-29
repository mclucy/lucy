//go:build !linux

package javainstall

import (
	"context"
	"errors"
	"io"
)

// addRepositoryKey is unreachable on this platform: only apt and dnf plans
// carry a repository key, and those package managers run on Linux only.
func addRepositoryKey(
	_ context.Context,
	_ *RepositoryKey,
	_ Policy,
	_ *Host,
	_ io.Reader,
	_ io.Writer,
) error {
	return errors.New("repository keys are installed on Linux only")
}
