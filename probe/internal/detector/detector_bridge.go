package detector

import (
	"archive/zip"
	"strings"
)

type BridgeMarker struct {
	NodeID string
	Risk   int
}

// DetectBridgeMarkers scans the JAR zip entries for known bridge/proxy/protocol-bridge indicators.
// Returns all markers found (may be empty). Never returns nil.
// This is evidence-only detection - presence of a marker means the JAR contains the software,
// not that compatibility is guaranteed.
func DetectBridgeMarkers(zipReader *zip.Reader) []BridgeMarker {
	markers := make(map[string]BridgeMarker)

	addMarker := func(nodeID string, risk int) {
		if _, exists := markers[nodeID]; exists {
			return
		}
		markers[nodeID] = BridgeMarker{NodeID: nodeID, Risk: risk}
	}

	for _, entry := range zipReader.File {
		entryName := entry.Name

		if strings.Contains(entryName, "dev/su5ed/sinytra/connector/") ||
			strings.Contains(entryName, "META-INF/services/dev.su5ed.sinytra.connector.api.ConnectorPlugin") ||
			strings.Contains(entryName, "connector.mixins.json") {
			addMarker("connector", 3)
		}

		if strings.Contains(entryName, "xyz/bluspring/kilt/") {
			addMarker("kilt", 3)
		}

		if strings.Contains(entryName, "com/velocitypowered/proxy/") {
			addMarker("velocity", 0)
		}

		if strings.Contains(entryName, "net/md_5/bungee/") {
			addMarker("bungeecord", 0)
		}

		if strings.Contains(entryName, "org/geysermc/geyser/") {
			addMarker("geyser", 1)
		}
	}

	if len(markers) == 0 {
		return []BridgeMarker{}
	}

	result := make([]BridgeMarker, 0, len(markers))
	for _, nodeID := range []string{"connector", "kilt", "velocity", "bungeecord", "geyser"} {
		if marker, exists := markers[nodeID]; exists {
			result = append(result, marker)
		}
	}

	return result
}
