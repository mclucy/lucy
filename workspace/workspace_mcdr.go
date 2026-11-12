package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/version"
	"go.yaml.in/yaml/v4"
)

// WrapperInfo describes non-jar wrappers around a server directory.
type WrapperInfo struct {
	MCDR *MCDREnv `json:"mcdr,omitempty"`
}

type MCDREnv struct {
	Version version.Version `json:"version"`
	Config  *MCDRConfig     `json:"config,omitempty"`
}

// MCDRConfig is the subset of an MCDR config.yml file that Lucy consumes.
// Unknown keys stay ignored so an MCDR upgrade does not break observation.
type MCDRConfig struct {
	WorkingDirectory  string   `yaml:"working_directory"`
	PluginDirectories []string `yaml:"plugin_directories"`
}

func readMCDRConfig(path string) (*MCDRConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", path, err)
	}
	defer file.Close()

	config := &MCDRConfig{}
	if err := yaml.NewDecoder(file).Decode(config); err != nil {
		return nil, fmt.Errorf("cannot decode %s: %w", path, err)
	}
	return config, nil
}

// ServerDirectory returns the directory that holds the server at anchor. Only
// MCDR moves the server away from the anchor, and it uses its configured
// working directory. A missing or unreadable configuration leaves the anchor.
func ServerDirectory(anchor string) string {
	return readMCDRWorkingDirectory(anchor)
}

// readMCDRWorkingDirectory resolves the server directory of an MCDR anchor
// while decoding only working_directory. Activity checks must stay tolerant of
// unrelated malformed configuration fields.
func readMCDRWorkingDirectory(anchor string) string {
	file, err := os.Open(filepath.Join(anchor, mcdrConfigFileName))
	if err != nil {
		return anchor
	}
	defer file.Close()

	var config struct {
		WorkingDirectory string `yaml:"working_directory"`
	}
	if err := yaml.NewDecoder(file).Decode(&config); err != nil {
		return anchor
	}
	return resolveMCDRWorkingDirectory(anchor, config.WorkingDirectory)
}

func (c *MCDRConfig) workingDirectory(anchor string) string {
	if c == nil {
		return anchor
	}
	return resolveMCDRWorkingDirectory(anchor, c.WorkingDirectory)
}

// resolveMCDRWorkingDirectory resolves a configured working directory against
// the MCDR anchor.
func resolveMCDRWorkingDirectory(anchor, workingDirectory string) string {
	if workingDirectory == "" {
		return anchor
	}
	if filepath.IsAbs(workingDirectory) {
		return workingDirectory
	}
	return filepath.Join(anchor, workingDirectory)
}
