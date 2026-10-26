//go:build darwin

package telemetry

import "golang.org/x/sys/unix"

func osVersion() string {
	for _, name := range []string{"kern.osproductversion", "kern.osrelease"} {
		if value, err := unix.Sysctl(name); err == nil && value != "" {
			return value
		}
	}
	return ""
}
