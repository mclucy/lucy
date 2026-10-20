package manifest

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed manifest.schema.json
var Schema []byte

// SchemaURL points at the canonical hosted copy of the manifest schema. It is
// used for the schema header that Lucy writes into lucy.yaml.
const SchemaURL = "https://raw.githubusercontent.com/mclucy/lucy/main/manifest/manifest.schema.json"

// WriteSchema writes the embedded manifest schema to path.
func WriteSchema(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, Schema, 0o644)
}
