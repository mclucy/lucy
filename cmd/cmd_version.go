package cmd

import (
	"runtime/debug"
	"strings"
)

// buildVersion resolves the version and commit for binaries that were not
// linked by the release pipeline. Task and GoReleaser inject both values;
// a plain `go build` or `go install` carries them in build information
// instead, and a disabled VCS stamp stays unknown.
func buildVersion() (string, string) {
	displayVersion, displayCommit := version, commit
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return displayVersion, displayCommit
	}
	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if displayVersion == "" {
		switch {
		case revision != "":
			// A local build carries VCS stamps. Go may also stamp a module
			// pseudo-version from the nearest tag, which is not a release.
			displayVersion = "devel"
		case info.Main.Version != "" && info.Main.Version != "(devel)":
			displayVersion = strings.TrimPrefix(info.Main.Version, "v")
		}
	}
	if modified && displayVersion == "devel" {
		// Fang truncates the commit to a short SHA, so the dirty marker
		// belongs to the version.
		displayVersion = "devel-dirty"
	}
	if displayCommit == "" && revision != "" {
		if len(revision) > 7 {
			revision = revision[:7]
		}
		displayCommit = revision
	}
	return displayVersion, displayCommit
}
