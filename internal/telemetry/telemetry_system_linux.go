//go:build linux

package telemetry

import (
	"os"
	"strings"
)

func osVersion() string {
	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(content), "\n") {
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[strings.TrimSpace(name)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	for _, name := range []string{"VERSION_ID", "PRETTY_NAME", "BUILD_ID"} {
		if value := values[name]; value != "" {
			return value
		}
	}
	return ""
}
