package bootstrap

import (
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
// Methods that do not generate a launch script start the server JAR directly.
func LaunchFor(method server.BootstrapMethod) Launch {
	launch := Launch{Command: "java -jar server.jar nogui", Handler: "vanilla_handler"}
	switch method {
	case server.BootstrapMethodFabric:
		launch.Command = "sh run.sh"
	case server.BootstrapMethodForgeInstaller:
		launch.Command = "sh run.sh"
		launch.Handler = "forge_handler"
	case server.BootstrapMethodPaperclip:
		launch.Handler = "bukkit_handler"
	}
	return launch
}

// Outputs reports the server-relative paths a method writes besides its locked
// inputs. Installation compares them with tracked files, and observation does
// not derive ownership from them.
func Outputs(locked document.LockedServer) []string {
	switch locked.Bootstrapper {
	case server.BootstrapMethodPlain, server.BootstrapMethodPaperclip:
		return []string{"server.jar"}
	case server.BootstrapMethodFabric:
		version := locked.CoreVersion.String()
		return []string{
			"run.sh",
			"run.bat",
			"fabric-server-launch.jar",
			"fabric-server-launcher.properties",
			"libraries/net/fabricmc/fabric-loader/" + version + "/fabric-loader-" + version + ".jar",
		}
	case server.BootstrapMethodForgeInstaller:
		return []string{"run.sh", "run.bat"}
	default:
		return nil
	}
}

// SupersededCore reports the core file a method leaves behind after it
// materializes the canonical server.jar. Only Paperclip starts from a
// differently named upstream JAR.
func SupersededCore(locked document.LockedServer) (string, bool) {
	if locked.Bootstrapper != server.BootstrapMethodPaperclip {
		return "", false
	}
	filename := locked.Filename
	if filename == "" || filename == "server.jar" {
		return "", false
	}
	return filename, true
}
