package operation

import (
	"github.com/mclucy/lucy/internal/telemetry"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
)

// observeManifest records the server a command read, before the operation may
// change it. Telemetry never performs its own scan.
func observeManifest(document *manifest.Document) {
	if document == nil {
		return
	}
	telemetry.ObserveServer(
		document.Server.Core.Product.String(),
		document.Server.Minecraft.String(),
	)
}

func observeLock(document *lockfile.Document) {
	if document == nil {
		return
	}
	telemetry.ObserveServer(
		document.Server.Product.String(),
		document.Server.Minecraft.String(),
	)
}
