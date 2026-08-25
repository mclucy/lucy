package workspace

import (
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"gopkg.in/ini.v1"
)

// ModPath lists the directories that hold content packages for this
// observation. Loader runtimes use mods/. Bukkit family servers use
// plugins/. The list is empty when no runtime was identified.
func (ws Workspace) ModPath() []string {
	return packageSearchPaths(ws.Server(), ws.Root)
}

// packageSearchPaths maps full-support ecosystem offers onto conventional
// directory names. Duplicate names appear once, in offer order.
func packageSearchPaths(
	server *ServerInstance,
	root string,
) []string {
	if server == nil || !server.IsValid() {
		return nil
	}

	var paths []string
	for _, offer := range server.effectiveEcosystems() {
		if offer.Compatibility != types.CompatFull {
			continue
		}

		var path string
		switch offer.Ecosystem {
		case types.EcoFabric, types.EcoForge, types.EcoNeoforge:
			path = filepath.Join(root, "mods")
		case types.EcoBukkit, types.EcoPaper:
			path = filepath.Join(root, "plugins")
		default:
			continue
		}

		duplicate := slices.Contains(paths, path)
		if !duplicate {
			paths = append(paths, path)
		}
	}
	return paths
}

// SaveDir returns the world save directory from server.properties. The path
// is Root joined with the level-name property. SaveDir returns an empty
// string when no usable properties file exists. A missing level-name
// degrades to Root. This matches the runtime default of storing the world
// beside the server.
func (ws Workspace) SaveDir() string {
	properties := parseServerProperties(ws.Root)
	if properties == nil {
		// Write one note when a known runtime has no properties file. Do not
		// write notes for empty or ambiguous directories.
		if ws.Server() != nil {
			log.Info("this server is missing a server.properties")
		}
		return ""
	}
	return filepath.Join(ws.Root, properties["level-name"])
}

// parseServerProperties reads root/server.properties into a flat map of key
// and value pairs. It returns nil after any read error or parse error. A
// missing file is normal for new server directories.
func parseServerProperties(
	root string,
) fileschema.FileMinecraftServerProperties {
	file, err := ini.Load(filepath.Join(root, "server.properties"))
	if err != nil {
		return nil
	}

	properties := make(map[string]string, len(file.Sections())*8)
	for _, section := range file.Sections() {
		for _, key := range section.Keys() {
			properties[key.Name()] = key.String()
		}
	}

	return properties
}
