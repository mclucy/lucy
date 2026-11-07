package mcdr

import "errors"

var (
	ErrorGhApi        = errors.New("error from GitHub API")
	ErrPluginNotFound = func(id string) error { return errors.New("plugin not found: " + id) }
)
