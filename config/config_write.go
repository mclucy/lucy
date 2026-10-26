package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

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
	directory := filepath.Dir(s.globalPath)
	if err := fsutil.MkdirAllPrivate(directory); err != nil {
		return nil, err
	}
	return fsutil.TryLockFile(filepath.Join(directory, "writer.lock"))
}

func (s *Service) Create(scope Scope, force bool) (result *WriteResult, resultErr error) {
	writer, err := s.lock(scope)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	file, err := s.file(scope)
	if err != nil {
		return nil, err
	}
	_, err = os.Lstat(file.Path())
	created := errors.Is(err, os.ErrNotExist)
	if err != nil && !created {
		return nil, err
	}
	if !created && !force {
		return nil, fmt.Errorf("config %s already exists; use --force to replace it with a fresh config", file.Path())
	}
	if force {
		err = file.Write(&Values{})
	} else {
		err = file.Create(&Values{})
	}
	if err != nil {
		return nil, err
	}
	log.Info("config created", "path", file.Path(), "replaced", !created)
	return &WriteResult{Path: file.Path(), Created: created}, nil
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
	writer, err := s.lock(scope)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	file, err := s.file(scope)
	if err != nil {
		return nil, err
	}
	values, err := file.Read()
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
	if created {
		err = file.Create(values)
	} else {
		err = file.Write(values)
	}
	if err != nil {
		return nil, err
	}
	log.Info("config updated", "path", file.Path(), "key", key, "created", created)
	return &WriteResult{Path: file.Path(), Created: created}, nil
}
