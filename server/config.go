package server

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/workspace"
	"gopkg.in/yaml.v3"
)

const (
	FormatVersion = "1"

	DefaultRunUser     = "minecraft"
	DefaultGroup       = "lucy"
	DefaultStopCommand = "stop"
)

var instanceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type Instance struct {
	FormatVersion    string `yaml:"format_version"`
	Name             string `yaml:"name"`
	Root             string `yaml:"root"`
	RunUser          string `yaml:"run_user"`
	RuntimeConfig    string `yaml:"runtime_config"`
	SystemdService   string `yaml:"systemd_service"`
	LaunchdLabel     string `yaml:"launchd_label"`
	RegisteredAt     string `yaml:"registered_at"`
	RegisteredByLucy string `yaml:"registered_by_lucy,omitempty"`
}

type RuntimeConfig struct {
	FormatVersion string            `yaml:"format_version"`
	WorkingDir    string            `yaml:"working_dir"`
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args"`
	Java          JavaConfig        `yaml:"java"`
	Memory        MemoryConfig      `yaml:"memory"`
	Env           map[string]string `yaml:"env,omitempty"`
	Logs          LogConfig         `yaml:"logs"`
	Stop          StopConfig        `yaml:"stop"`
	Restart       RestartConfig     `yaml:"restart"`
}

type JavaConfig struct {
	Path string `yaml:"path"`
}

type MemoryConfig struct {
	Min string `yaml:"min,omitempty"`
	Max string `yaml:"max,omitempty"`
}

type LogConfig struct {
	ConsolePath string `yaml:"console_path"`
}

type StopConfig struct {
	Command string `yaml:"command"`
	Timeout string `yaml:"timeout"`
}

type RestartConfig struct {
	Policy string `yaml:"policy"`
}

// ValidateInstanceName rejects names that cannot safely identify registry files
// and native services; the first character must be a lowercase letter or digit.
func ValidateInstanceName(name string) error {
	if !instanceNamePattern.MatchString(name) {
		return fmt.Errorf("invalid server name %q: use lowercase letters, digits, '-' or '_'", name)
	}
	return nil
}

// NewInstance builds registry metadata with an absolute root and default run user.
// It validates the name but does not create files or install a service.
func NewInstance(name, root, runUser string) (Instance, error) {
	if err := ValidateInstanceName(name); err != nil {
		return Instance{}, err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Instance{}, fmt.Errorf("resolve server root: %w", err)
	}
	if runUser == "" {
		runUser = DefaultRunUser
	}
	return Instance{
		FormatVersion:  FormatVersion,
		Name:           name,
		Root:           absRoot,
		RunUser:        runUser,
		RuntimeConfig:  filepath.Join(absRoot, LocalConfigFile),
		SystemdService: SystemdInstanceServiceName(name),
		LaunchdLabel:   LaunchdInstanceLabel(name),
		RegisteredAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// GuessRuntimeConfig derives a launch command from the detected server artifact,
// falling back to Java and server.jar when detection provides no entry point.
func GuessRuntimeConfig(root string) RuntimeConfig {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}

	cfg := RuntimeConfig{
		FormatVersion: FormatVersion,
		WorkingDir:    absRoot,
		Command:       "java",
		Args:          []string{"-jar", "server.jar", "nogui"},
		Java:          JavaConfig{Path: "java"},
		Memory:        MemoryConfig{Min: "1G", Max: "4G"},
		Env:           map[string]string{},
		Logs: LogConfig{
			ConsolePath: filepath.Join(absRoot, "logs", "lucy-console.log"),
		},
		Stop: StopConfig{
			Command: DefaultStopCommand,
			Timeout: "60s",
		},
		Restart: RestartConfig{Policy: "on-failure"},
	}

	ws := workspace.NewAt(absRoot)
	server := ws.Server()
	if server == nil || server.PrimaryPath == "" {
		return cfg
	}

	entrance := server.PrimaryPath
	if !filepath.IsAbs(entrance) {
		entrance = filepath.Join(absRoot, entrance)
	}
	relEntrance := entrance
	if rel, err := filepath.Rel(absRoot, entrance); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		relEntrance = rel
	}

	switch strings.ToLower(filepath.Ext(entrance)) {
	case ".jar":
		cfg.Command = cfg.Java.Path
		cfg.Args = []string{
			"-Xms" + cfg.Memory.Min,
			"-Xmx" + cfg.Memory.Max,
			"-jar",
			relEntrance,
			"nogui",
		}
	default:
		cfg.Command = relEntrance
		if !filepath.IsAbs(relEntrance) {
			cfg.Command = "." + string(os.PathSeparator) + relEntrance
		}
		cfg.Args = []string{"nogui"}
	}
	return cfg
}

// ReadRuntimeConfig loads launch settings and fills omitted defaults relative to
// the config directory; a missing file returns nil without an error.
func ReadRuntimeConfig(path string) (*RuntimeConfig, error) {
	data, ok, err := state.SafeRead(path)
	if err != nil || !ok {
		return nil, err
	}
	var cfg RuntimeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse runtime config: %w", err)
	}
	normalizeRuntimeConfig(&cfg, filepath.Dir(path))
	return &cfg, nil
}

// WriteRuntimeConfig normalizes cfg in place and atomically replaces its YAML
// file, creating parent directories and rejecting a nil configuration.
func WriteRuntimeConfig(path string, cfg *RuntimeConfig) error {
	if cfg == nil {
		return fmt.Errorf("runtime config is nil")
	}
	normalizeRuntimeConfig(cfg, filepath.Dir(path))
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("serialize runtime config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create runtime config directory: %w", err)
	}
	return state.AtomicWrite(path, data, 0o644)
}

// normalizeRuntimeConfig supplies omitted launch, logging and stop defaults in
// place, using fallbackRoot only when no working directory is configured.
func normalizeRuntimeConfig(cfg *RuntimeConfig, fallbackRoot string) {
	if cfg.FormatVersion == "" {
		cfg.FormatVersion = FormatVersion
	}
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = fallbackRoot
	}
	if cfg.Java.Path == "" {
		cfg.Java.Path = "java"
	}
	if cfg.Memory.Min == "" {
		cfg.Memory.Min = "1G"
	}
	if cfg.Memory.Max == "" {
		cfg.Memory.Max = "4G"
	}
	if cfg.Logs.ConsolePath == "" {
		cfg.Logs.ConsolePath = filepath.Join(cfg.WorkingDir, "logs", "lucy-console.log")
	}
	if cfg.Stop.Command == "" {
		cfg.Stop.Command = DefaultStopCommand
	}
	if cfg.Stop.Timeout == "" {
		cfg.Stop.Timeout = "60s"
	}
	if cfg.Restart.Policy == "" {
		cfg.Restart.Policy = "on-failure"
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
}
