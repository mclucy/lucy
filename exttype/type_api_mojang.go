package exttype

import "time"

// ApiMojangMinecraftVersionManifest https://piston-meta.mojang.com/mc/game/version_manifest_v2.json
type ApiMojangMinecraftVersionManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []struct {
		Id              string    `json:"id"`
		Type            string    `json:"type"`
		Url             string    `json:"url"`
		Time            time.Time `json:"time"`
		ReleaseTime     time.Time `json:"releaseTime"`
		Sha1            string    `json:"sha1"`
		ComplianceLevel int       `json:"complianceLevel"`
	} `json:"versions"`
}
