package cmd

import (
	"runtime/debug"
	"strings"
)

// buildVersion resolves the display identity from linker flags or Go build information.
func buildVersion() (string, string) {
	displayVersion, displayCommit := version, commit
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return displayVersion, displayCommit
	}
	revision := ""
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		}
	}
	if displayVersion == "" && revision == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		displayVersion = strings.TrimPrefix(info.Main.Version, "v")
	}
	if displayCommit == "" && revision != "" {
		if len(revision) > 7 {
			revision = revision[:7]
		}
		displayCommit = revision
	}
	return displayVersion, displayCommit
}

func isTelemetryRelease() bool {
	if telemetryRelease == "true" {
		return true
	}
	info, ok := debug.ReadBuildInfo()
	return ok && info.Main.Sum != "" && info.Main.Version != "" && info.Main.Version != "(devel)"
}
