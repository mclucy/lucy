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

func EtcDir() string {
	if v := os.Getenv("LUCY_ETC_DIR"); v != "" {
		return v
	}
	return defaultEtcDir
}

func RunDir() string {
	if v := os.Getenv("LUCY_RUN_DIR"); v != "" {
		return v
	}
	return defaultRunDir
}

func StateDir() string {
	if v := os.Getenv("LUCY_STATE_DIR"); v != "" {
		return v
	}
	return defaultStateDir
}

func LogDir() string {
	if v := os.Getenv("LUCY_LOG_DIR"); v != "" {
		return v
	}
	return defaultLogDir
}

func ServersDir() string {
	return filepath.Join(EtcDir(), "servers.d")
}

func RuntimeStateDir() string {
	return filepath.Join(StateDir(), "state")
}

func DaemonSocketPath() string {
	return filepath.Join(RunDir(), "lucyd.sock")
}

func RunnerSocketDir() string {
	return filepath.Join(RunDir(), "servers")
}

func RunnerSocketPath(name string) string {
	return filepath.Join(RunnerSocketDir(), name+".sock")
}

func InstanceRegistryPath(name string) string {
	return filepath.Join(ServersDir(), name+".yaml")
}

func RuntimeStatePath(name string) string {
	return filepath.Join(RuntimeStateDir(), name+".json")
}
