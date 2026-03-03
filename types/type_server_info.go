package types

import (
	"os/exec"

	"github.com/mclucy/lucy/exttype"
)

// ServerInfo components that do not exist, use an empty string. Note Executable
// must exist, otherwise the program will exit; therefore, it is not a pointer.
type ServerInfo struct {
	WorkPath     string
	SavePath     string
	ModPath      []string
	Packages     []Package
	Executable   *ExecutableInfo
	Activity     *ServerActivity
	Environments EnvironmentInfo
}

type ExecutableInfo struct {
	Path          string
	GameVersion   RawVersion
	ModLoader     Platform
	LoaderVersion RawVersion
	BootCommand   *exec.Cmd
}

type ServerActivity struct {
	Active bool
	Pid    int
}

type EnvironmentInfo struct {
	Lucy *LucyEnv
	Mcdr *McdrEnv
}

type McdrEnv struct {
	Version RawVersion
	Config  *exttype.FileMcdrConfig
}

// LucyEnv is a placeholder for Lucy environment; currently just a boolean
// indicating presence, but can be expanded with more details if needed
type LucyEnv bool
