package exttype

import (
	"encoding/json"
	"time"
)

// FileMinecraftVersionSpec is the struct for version.json found in Minecraft
// jar files.
//
// wiki:
// https://zh.minecraft.wiki/w/%E7%89%88%E6%9C%AC%E4%BF%A1%E6%81%AF%E6%96%87%E4%BB%B6%E6%A0%BC%E5%BC%8F
//
// TODO: This file does not exist before 18w47b (1.14), find alternative methods to detect versions
type FileMinecraftVersionSpec struct {
	Id              string          `json:"id"`
	Name            string          `json:"name"`
	WorldVersion    int             `json:"world_version"`
	SeriesId        string          `json:"series_id"`
	ReleaseTarget   string          `json:"release_target"` // removed in 22w42a
	ProtocolVersion int             `json:"protocol_version"`
	PackVersion     json.RawMessage `json:"pack_version"` // varies across versions
	BuildTime       time.Time       `json:"build_time"`
	JavaComponent   string          `json:"java_component"`
	JavaVersion     int             `json:"java_version"`
	Stable          bool            `json:"stable"`
	UseEditor       bool            `json:"use_editor"`
}

// FileMinecraftServerProperties is the struct for server.properties
type FileMinecraftServerProperties map[string]string
