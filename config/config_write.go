package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
)

func (s *Service) lock(scope Scope) (*fsutil.FileLock, error) {
	if scope == WorkspaceScope {
		if err := workspacewrite.SafePath(s.root, WorkspaceFilename); err != nil {
			return nil, err
		}
		writer, err := workspacewrite.Lock(s.root)
		if err != nil {
			return nil, err
		}
		if err := workspacewrite.RecoverInstall(s.root); err != nil {
			return nil, errors.Join(err, writer.Close())
		}
		return writer, nil
	}
	if scope != GlobalScope {
		return nil, fmt.Errorf("unknown config scope")
	}
	if s.globalErr != nil {
		return nil, s.globalErr
	}
	directory := filepath.Dir(s.globalPath)
	if err := fsutil.MkdirAllPrivate(directory); err != nil {
		return nil, err
	}
	return fsutil.TryLockFile(filepath.Join(directory, "writer.lock"))
}

// workspaceRoot opens the workspace directory that confines configuration IO.
// Global configuration has no such root because only this user can change its
// private directory.
func (s *Service) workspaceRoot(scope Scope) (*os.Root, error) {
	if scope != WorkspaceScope {
		return nil, nil
	}
	return os.OpenRoot(s.root)
}

// absent reports whether the configuration file is free. A workspace check runs
// through its root handle, so a replaced parent cannot answer for a foreign
// file.
func absent(file document.Service[Values], root *os.Root) (bool, error) {
	var err error
	if root == nil {
		_, err = os.Lstat(file.Path())
	} else {
		_, err = root.Lstat(WorkspaceFilename)
	}
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, os.ErrNotExist):
		return true, nil
	default:
		return false, err
	}
}

// readValues reads through root when the scope has one, so a replaced parent
// cannot feed a write with a foreign document.
func readValues(file document.Service[Values], root *os.Root) (*Values, error) {
	if root == nil {
		return file.Read()
	}
	return file.ReadRoot(root, WorkspaceFilename)
}

// writeValues publishes through root when the scope has one, keeping reading,
// staging, and renaming on the handle the writer lock verified.
func writeValues(file document.Service[Values], root *os.Root, values *Values, replace bool) error {
	if root == nil {
		if replace {
			return file.Write(values)
		}
		return file.Create(values)
	}
	if replace {
		return file.WriteRoot(root, WorkspaceFilename, values)
	}
	return file.CreateRoot(root, WorkspaceFilename, values)
}

// CreateWorkspace publishes a fresh workspace configuration when the file is
// absent and reports whether it wrote one. The write stays confined to the
// workspace root.
func CreateWorkspace(root string, plain bool) (created bool, resultErr error) {
	files, err := os.OpenRoot(root)
	if err != nil {
		return false, err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	file := configFile(filepath.Join(root, filepath.FromSlash(WorkspaceFilename)))
	if plain {
		file = file.WithEncoder((*Values).PlainBytes)
	}
	err = file.CreateRoot(files, WorkspaceFilename, &Values{})
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

type CreateOptions struct {
	Force      bool
	NoComments bool
}

func (s *Service) Create(scope Scope, options CreateOptions) (result *WriteResult, resultErr error) {
	writer, err := s.lock(scope)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	file, err := s.file(scope)
	if err != nil {
		return nil, err
	}
	if options.NoComments {
		file = file.WithEncoder((*Values).PlainBytes)
	}
	// Create replaces the file by definition: a forced create must not merge
	// its empty template into the values it is discarding.
	file = file.WithoutMerge()
	root, err := s.workspaceRoot(scope)
	if err != nil {
		return nil, err
	}
	if root != nil {
		defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	}
	created, err := absent(file, root)
	if err != nil {
		return nil, err
	}
	if !created && !options.Force {
		return nil, fmt.Errorf("config %s already exists; use --force to replace it with a fresh config", file.Path())
	}
	if err := writeValues(file, root, &Values{}, options.Force); err != nil {
		return nil, err
	}
	log.Info("config created", "path", file.Path(), "replaced", !created)
	return &WriteResult{Path: file.Path(), Created: created}, nil
}

// validateSetting rejects values that the field's meaning cannot accept.
func validateSetting(key, text string) error {
	switch key {
	case "github.proxy":
		return validateProxyURL(text)
	default:
		return nil
	}
}

func validateProxyURL(text string) error {
	if text == "" {
		return nil
	}
	parsed, err := url.Parse(text)
	if err != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" {
		return fmt.Errorf("config github.proxy must be an absolute HTTP(S) URL")
	}
	return nil
}

func (s *Service) Set(key, text string, scope Scope) (result *WriteResult, resultErr error) {
	probe, err := fieldValue(&Values{}, key)
	if err != nil {
		return nil, err
	}
	if probe.Kind() != reflect.Pointer || probe.Type().Elem().Kind() != reflect.String {
		return nil, fmt.Errorf("config key %q requires a field, not a section", key)
	}
	if strings.ContainsRune(text, '\x00') {
		return nil, fmt.Errorf("config %s cannot contain a null character", key)
	}
	if err := validateSetting(key, text); err != nil {
		return nil, err
	}
	writer, err := s.lock(scope)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	file, err := s.file(scope)
	if err != nil {
		return nil, err
	}
	root, err := s.workspaceRoot(scope)
	if err != nil {
		return nil, err
	}
	if root != nil {
		defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	}
	values, err := readValues(file, root)
	created := errors.Is(err, os.ErrNotExist)
	if created {
		values = &Values{}
	} else if err != nil {
		return nil, fmt.Errorf("read config %s: %w", file.Path(), err)
	}
	field, err := fieldValue(values, key)
	if err != nil {
		return nil, err
	}
	field.Set(reflect.ValueOf(new(text)))
	if err := writeValues(file, root, values, !created); err != nil {
		return nil, err
	}
	log.Info("config updated", "path", file.Path(), "key", key, "created", created)
	return &WriteResult{Path: file.Path(), Created: created}, nil
}
