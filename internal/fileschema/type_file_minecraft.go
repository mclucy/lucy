package fileschema

import (
	"encoding/json/jsontext"
	"time"
)

// FileMinecraftVersionSpec is version.json metadata found in Minecraft JARs
// from 18w47b onward.
type FileMinecraftVersionSpec struct {
	Id              string         `json:"id"`
	Name            string         `json:"name"`
	WorldVersion    int            `json:"world_version"`
	SeriesId        string         `json:"series_id"`
	ReleaseTarget   string         `json:"release_target"` // removed in 22w42a
	ProtocolVersion int            `json:"protocol_version"`
	PackVersion     jsontext.Value `json:"pack_version"` // varies across versions
	BuildTime       time.Time      `json:"build_time"`
	JavaComponent   string         `json:"java_component"`
	JavaVersion     int            `json:"java_version"`
	Stable          bool           `json:"stable"`
	UseEditor       bool           `json:"use_editor"`
}

// FileMinecraftServerProperties is the struct for server.properties
type FileMinecraftServerProperties map[string]string
