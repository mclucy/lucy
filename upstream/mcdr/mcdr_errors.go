package mcdr

import "errors"

var (
	ErrorGhApi         = errors.New("error from GitHub API")
	ErrPluginNotFound  = func(id string) error { return errors.New("plugin not found: " + id) }
	ErrVersionNotFound = func(id string, version string) error {
		return errors.New("version not found: " + version + " for plugin " + id)
	}
)
