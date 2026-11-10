package bootstrap

import (
	"slices"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
)

// Launch describes how a prepared server tree starts.
type Launch struct {
	// Command runs the server from its directory.
	Command string
	// Handler is the MCDReforged log handler that parses the server stream.
	Handler string
}

// LaunchFor reports the start command and log handler of a bootstrap method.
// Methods that generate no launcher of their own start server.jar directly.
func LaunchFor(method server.BootstrapMethod) Launch {
	launch := Launch{Command: "java -jar server.jar nogui", Handler: "vanilla_handler"}
	switch method {
	case server.BootstrapMethodFabric:
		launch.Command = "sh run.sh"
	case server.BootstrapMethodQuilt:
		launch.Command = "java -jar quilt-server-launch.jar nogui"
	case server.BootstrapMethodForgeInstaller:
		launch.Command = "sh run.sh"
		launch.Handler = "forge_handler"
	case server.BootstrapMethodPaperclip:
		launch.Handler = "bukkit_handler"
	}
	return launch
}

// Outputs reports the known server-relative managed paths of a method.
// Preflight validation and staged output classification share this contract.
func Outputs(locked document.LockedServer) ([]string, error) {
	exact, _, err := validateContract(locked)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, spec := range exact {
		if spec.Policy != PolicyManaged {
			continue
		}
		paths = append(paths, spec.Path)
	}
	slices.Sort(paths)
	return paths, nil
}

// RequiredSeedOutputs reports the server-relative seed paths that are strictly
// required for server launch bootability (e.g. user_jvm_args.txt for Forge).
func RequiredSeedOutputs(locked document.LockedServer) []string {
	switch locked.Bootstrapper {
	case server.BootstrapMethodForgeInstaller:
		return []string{"user_jvm_args.txt"}
	default:
		return nil
	}
}
