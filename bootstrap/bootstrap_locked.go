package bootstrap

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	serverpkg "github.com/mclucy/lucy/server"
)

// PrepareLocked builds the server tree of a locked server in stage. java is the
// Java executable for the bootstrap steps that run Java; it may be empty when
// no runtime is installed, and only those steps fail then.
func PrepareLocked(ctx context.Context, stage string, server document.LockedServer, corePath string, inputs map[string]string, java string, output io.Writer) error {
	if err := document.SafeRelativePath(server.Directory); err != nil {
		return err
	}
	root := filepath.Join(stage, server.Directory)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	var installer *zip.ReadCloser
	if server.Bootstrapper == serverpkg.BootstrapMethodForgeInstaller {
		var err error
		installer, err = zip.OpenReader(corePath)
		if err != nil {
			return err
		}
		defer installer.Close()
	}
	for _, input := range server.Inputs {
		if err := document.SafeRelativePath(input.Path); err != nil {
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
		if err := fsutil.AtomicCopyFile(src, dst, 0o644); err != nil {
			return err
		}
	}
	switch server.Bootstrapper {
	case serverpkg.BootstrapMethodPlain:
		return preparePlain(root, corePath)
	case serverpkg.BootstrapMethodPaperclip:
		return preparePaperclip(ctx, root, corePath, java, output)
	case serverpkg.BootstrapMethodFabric:
		return prepareFabric(root, server, corePath)
	case serverpkg.BootstrapMethodForgeInstaller:
		return prepareForgeInstaller(ctx, root, server, corePath, &installer.Reader, java, output)
	default:
		return fmt.Errorf("unsupported bootstrap adapter %q", server.Bootstrapper)
	}
}

func extractLockedEntry(z *zip.Reader, name, dst string) error {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return errors.Join(err, r.Close())
		}
		out, err := os.Create(dst)
		if err != nil {
			return errors.Join(err, r.Close())
		}
		_, err = io.Copy(out, r)
		if err != nil {
			return errors.Join(err, r.Close(), out.Close())
		}
		return errors.Join(r.Close(), out.Close())
	}
	return fmt.Errorf("missing pinned installer entry %s", name)
}

// runLockedProcess streams a subprocess's combined output to output while
// retaining it for the error message. It returns the captured output.
func runLockedProcess(cmd *exec.Cmd, output io.Writer) ([]byte, error) {
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

// ErrNoJava reports a bootstrap step that runs Java when no Java runtime is
// installed.
var ErrNoJava = errors.New("no Java runtime is installed")

func requireJava(java, step string) error {
	if java == "" {
		return fmt.Errorf("%s needs a Java runtime: %w", step, ErrNoJava)
	}
	return nil
}
