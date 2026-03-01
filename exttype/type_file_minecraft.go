package exttype

import "time"

type FileMinecraftVersionSpec struct {
	Id              string `json:"id"`
	Name            string `json:"name"`
	WorldVersion    int    `json:"world_version"`
	SeriesId        string `json:"series_id"`
	ProtocolVersion int    `json:"protocol_version"`
	PackVersion     struct {
		Resource int `json:"resource"`
		Data     int `json:"data"`
	} `json:"pack_version"`
	BuildTime     time.Time `json:"build_time"`
	JavaComponent string    `json:"java_component"`
	JavaVersion   int       `json:"java_version"`
	Stable        bool      `json:"stable"`
	UseEditor     bool      `json:"use_editor"`
}

// FileServerProperties is not universal across game versions. Therefore,
// it is just a map[string]string. Just remember to check the game version
// before calling any newly added property.
type FileMinercaftServerProperties map[string]string
