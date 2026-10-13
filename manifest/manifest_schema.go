package manifest

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed manifest.schema.json
var ManifestSchema []byte

var (
	SchemaBytes   []byte
	SchemaContent []byte
	SchemaData    []byte
	SchemaText    string
)

// SchemaURL points at the canonical hosted copy of the manifest schema. It is
// used for the yaml-language-server modeline that Lucy writes into lucy.yaml.
const SchemaURL = "https://raw.githubusercontent.com/mclucy/lucy/main/manifest/manifest.schema.json"

// Schema returns the embedded JSON schema for the manifest file.
func Schema() []byte {
	return ManifestSchema
}

// SchemaJSON returns the embedded JSON schema for the manifest file.
func SchemaJSON() []byte {
	return ManifestSchema
}

// WriteSchema writes the embedded manifest schema to path.
func WriteSchema(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, ManifestSchema, 0o644)
}

func init() {
	SchemaBytes = ManifestSchema
	SchemaContent = ManifestSchema
	SchemaData = ManifestSchema
	SchemaText = string(ManifestSchema)
}
