package upstream

import "time"

// Build describes a published project version and its known compatibility.
type Build struct {
	Version      string
	GameVersions []string
	Loaders      []string
	Channel      string
	Published    time.Time
}
