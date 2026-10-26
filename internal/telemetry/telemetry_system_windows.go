//go:build windows

package telemetry

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func osVersion() string {
	info := windows.RtlGetVersion()
	if info == nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", info.MajorVersion, info.MinorVersion, info.BuildNumber)
}
