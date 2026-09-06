package server

import (
	"os"
	"path/filepath"
)

const (
	LocalConfigFile = "lucy-server.yaml"

	defaultEtcDir   = "/etc/lucy"
	defaultRunDir   = "/run/lucy"
	defaultStateDir = "/var/lib/lucy"
	defaultLogDir   = "/var/log/lucy"
)

// EtcDir returns the registry configuration root, honoring LUCY_ETC_DIR.
func EtcDir() string {
	if v := os.Getenv("LUCY_ETC_DIR"); v != "" {
		return v
	}
	return defaultEtcDir
}

// RunDir returns the socket and lock root, honoring LUCY_RUN_DIR.
func RunDir() string {
	if v := os.Getenv("LUCY_RUN_DIR"); v != "" {
		return v
	}
	return defaultRunDir
}

// StateDir returns the persistent daemon data root, honoring LUCY_STATE_DIR.
func StateDir() string {
	if v := os.Getenv("LUCY_STATE_DIR"); v != "" {
		return v
	}
	return defaultStateDir
}

// LogDir returns the daemon log root, honoring LUCY_LOG_DIR.
func LogDir() string {
	if v := os.Getenv("LUCY_LOG_DIR"); v != "" {
		return v
	}
	return defaultLogDir
}

// ServersDir returns the directory containing instance registry YAML files.
func ServersDir() string {
	return filepath.Join(EtcDir(), "servers.d")
}

// RuntimeStateDir returns the directory containing per-instance restart markers.
func RuntimeStateDir() string {
	return filepath.Join(StateDir(), "state")
}

// DaemonSocketPath returns the shared daemon control socket path.
func DaemonSocketPath() string {
	return filepath.Join(RunDir(), "lucyd.sock")
}

// RunnerSocketDir returns the parent directory of instance control sockets.
func RunnerSocketDir() string {
	return filepath.Join(RunDir(), "servers")
}

// RunnerSocketPath derives a control socket path from an already validated name.
func RunnerSocketPath(name string) string {
	return filepath.Join(RunnerSocketDir(), name+".sock")
}

// InstanceRegistryPath derives a registry path from an already validated name.
func InstanceRegistryPath(name string) string {
	return filepath.Join(ServersDir(), name+".yaml")
}

// RuntimeStatePath derives a restart-marker path from an already validated name.
func RuntimeStatePath(name string) string {
	return filepath.Join(RuntimeStateDir(), name+".json")
}
