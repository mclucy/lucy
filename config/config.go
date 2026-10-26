// Package config manages machine-local global and workspace configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/mclucy/lucy/document"
)

const WorkspaceFilename = ".lucy/config.yaml"

type Scope uint8

const (
	WorkspaceScope Scope = iota
	GlobalScope
)

type Service struct {
	root       string
	globalPath string
}

type WriteResult struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
}

func New(root string) (*Service, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace directory: %w", err)
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("locate global config directory: %w", err)
	}
	return &Service{
		root:       absolute,
		globalPath: filepath.Join(directory, "lucy", "config.yaml"),
	}, nil
}

func WorkspaceFile(root string) document.Service[Values] {
	return configFile(filepath.Join(root, filepath.FromSlash(WorkspaceFilename)))
}

func configFile(path string) document.Service[Values] {
	return document.NewService(path, document.Codec[Values]{
		Decode: decodeValues,
		Encode: encodeValues,
	}).WithMode(0o600)
}

func (s *Service) file(scope Scope) (document.Service[Values], error) {
	switch scope {
	case WorkspaceScope:
		return WorkspaceFile(s.root), nil
	case GlobalScope:
		return configFile(s.globalPath), nil
	default:
		return document.Service[Values]{}, fmt.Errorf("unknown config scope")
	}
}

func (s *Service) read(scope Scope, required bool) (*Values, error) {
	file, err := s.file(scope)
	if err != nil {
		return nil, err
	}
	values, err := file.Read()
	if errors.Is(err, os.ErrNotExist) {
		if !required {
			return &Values{}, nil
		}
		command := "lucy config create"
		if scope == GlobalScope {
			command += " --global"
		}
		return nil, fmt.Errorf("config %s does not exist; run `%s` to create it: %w", file.Path(), command, err)
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", file.Path(), err)
	}
	return values, nil
}

func (s *Service) Get(key string, scope Scope) (any, error) {
	selected, err := s.read(scope, true)
	if err != nil {
		return nil, err
	}
	global, workspace := selected, &Values{}
	if scope == WorkspaceScope {
		workspace = selected
		global, err = s.read(GlobalScope, false)
		if err != nil {
			return nil, err
		}
	}
	merged := mergeValues(global, workspace)
	if key == "" {
		return merged, nil
	}
	value, err := fieldValue(merged, key)
	if err != nil {
		return nil, err
	}
	if value.Kind() == reflect.Pointer {
		return value.Elem().Interface(), nil
	}
	return value.Interface(), nil
}

// JavaHome resolves the selected path against the directory of its source
// scope. Missing config files do not disable automatic runtime selection.
func (s *Service) JavaHome() (home string, sourcePath string, err error) {
	global, err := s.read(GlobalScope, false)
	if err != nil {
		return "", "", err
	}
	workspace, err := s.read(WorkspaceScope, false)
	if err != nil {
		return "", "", err
	}
	selected, base, source := global.Java.Home, filepath.Dir(s.globalPath), s.globalPath
	if workspace.Java.Home != nil {
		selected, base, source = workspace.Java.Home, s.root, filepath.Join(s.root, WorkspaceFilename)
	}
	if selected == nil || *selected == "" {
		return "", "", nil
	}
	home = *selected
	if !filepath.IsAbs(home) {
		home = filepath.Join(base, home)
	}
	if info, statErr := os.Stat(home); statErr == nil && !info.IsDir() {
		home = filepath.Dir(filepath.Dir(home))
	}
	return filepath.Clean(home), source, nil
}
