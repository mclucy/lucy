package document

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed document_manifest.schema.json
var ManifestSchema []byte

// ManifestSchemaURL points at the canonical hosted copy of the manifest
// schema. It is used for the schema header that Lucy writes into lucy.yaml.
const ManifestSchemaURL = "https://raw.githubusercontent.com/mclucy/lucy/main/document/document_manifest.schema.json"

// WriteManifestSchema writes the embedded manifest schema to path.
func WriteManifestSchema(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, ManifestSchema, 0o644)
}
