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
	globalErr  error
}

type WriteResult struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
}

// New builds the service for root. An unavailable user configuration directory
// only fails requests for the global scope, so Java selection still works in
// environments without a home directory.
func New(root string) (*Service, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace directory: %w", err)
	}
	service := &Service{root: absolute}
	directory, err := os.UserConfigDir()
	if err != nil {
		service.globalErr = fmt.Errorf("locate global config directory: %w", err)
		return service, nil
	}
	service.globalPath = filepath.Join(directory, "lucy", "config.yaml")
	return service, nil
}

func configFile(path string) document.Service[Values] {
	return document.NewService(path, document.Codec[Values]{
		Decode: decodeValues,
		Encode: encodeValues,
		Merge:  mergeConfigBytes,
	}).WithMode(0o600)
}

func (s *Service) file(scope Scope) (document.Service[Values], error) {
	switch scope {
	case WorkspaceScope:
		return configFile(filepath.Join(s.root, filepath.FromSlash(WorkspaceFilename))), nil
	case GlobalScope:
		if s.globalErr != nil {
			return document.Service[Values]{}, s.globalErr
		}
		return configFile(s.globalPath), nil
	default:
		return document.Service[Values]{}, fmt.Errorf("unknown config scope")
	}
}

func (s *Service) read(scope Scope, required bool) (*Values, error) {
	file, err := s.file(scope)
	if err != nil {
		if scope == GlobalScope && !required {
			return &Values{}, nil
		}
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

// Setting returns one effective string setting. Missing layers are empty, and
// a workspace value overrides a global one.
func (s *Service) Setting(key string) (string, error) {
	global, err := s.read(GlobalScope, false)
	if err != nil {
		return "", err
	}
	workspace, err := s.read(WorkspaceScope, false)
	if err != nil {
		return "", err
	}
	merged := mergeValues(global, workspace)
	value, err := fieldValue(merged, key)
	if err != nil {
		return "", err
	}
	if value.Kind() != reflect.Pointer {
		return "", fmt.Errorf("config key %q is not a string", key)
	}
	if value.IsNil() {
		return "", nil
	}
	return value.Elem().String(), nil
}

// GitHubProxy returns the configured prefix proxy for GitHub release
// downloads. An empty prefix downloads directly.
func (s *Service) GitHubProxy() (string, error) {
	return s.Setting("github.proxy")
}

// CurseForgeAPIKey returns the configured CurseForge API key. An empty key
// lets the provider use the key embedded in this build.
func (s *Service) CurseForgeAPIKey() (string, error) {
	return s.Setting("curseforge.api_key")
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
