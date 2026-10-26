package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/log"
)

const activeMaxAge = time.Minute

type activeMarker struct {
	PID int `json:"pid"`
}

// registerActive records one upload so `off` can cancel it. Failure to record
// the marker only weakens cancellation; it does not block the upload.
func registerActive() func() {
	root, err := activeRoot()
	if err != nil {
		return func() {}
	}
	if err := ensurePrivateDirectory(root); err != nil {
		log.Debug("telemetry cancel directory unavailable", "error", err)
		return func() {}
	}
	suffix, err := randomHex(8)
	if err != nil {
		return func() {}
	}
	path := filepath.Join(root, strconv.Itoa(os.Getpid())+"-"+suffix+".json")
	content, err := json.Marshal(activeMarker{PID: os.Getpid()})
	if err != nil {
		return func() {}
	}
	if err := writePrivateFile(path, content); err != nil {
		log.Debug("telemetry cancel marker unavailable", "error", err)
		return func() {}
	}
	return func() { _ = os.Remove(path) }
}

// cancelActive terminates every upload the current user registered.
func cancelActive() {
	root, err := activeRoot()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > activeMaxAge {
			_ = os.Remove(path)
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var marker activeMarker
		if err := json.Unmarshal(content, &marker); err != nil {
			_ = os.Remove(path)
			continue
		}
		if marker.PID > 0 && marker.PID != os.Getpid() {
			if process, err := os.FindProcess(marker.PID); err == nil {
				if err := process.Kill(); err != nil {
					log.Debug("telemetry upload could not be canceled", "pid", marker.PID, "error", err)
				}
			}
		}
		_ = os.Remove(path)
	}
}
