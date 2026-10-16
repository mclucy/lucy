package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"reflect"

	"github.com/invopop/jsonschema"
	"github.com/mclucy/lucy/manifest"
	orderedmap "github.com/pb33f/ordered-map/v2"
)

const schemaURL = "https://raw.githubusercontent.com/mclucy/lucy/main/manifest/manifest.schema.json"

func main() {
	path := "manifest/manifest.schema.json"

	requirementSchema := &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			{
				Type:        "string",
				Description: "A version selector: stable, a tag, or an exact version.",
			},
			func() *jsonschema.Schema {
				version := &jsonschema.Schema{
					Type:        "string",
					Description: "A version selector: stable, a tag, or an exact version.",
				}
				file := &jsonschema.Schema{
					Type:        "string",
					Description: "Workspace-relative path to a local package artifact.",
				}
				enabled := &jsonschema.Schema{
					Type:        "boolean",
					Default:     true,
					Description: "Whether the requirement is active in the resolved environment.",
				}
				properties := orderedmap.New[string, *jsonschema.Schema]()
				properties.Set("version", version)
				properties.Set("file", file)
				properties.Set("enabled", enabled)
				return &jsonschema.Schema{
					Type:                 "object",
					Properties:           properties,
					AdditionalProperties: jsonschema.FalseSchema,
				}
			}(),
		},
	}

	r := &jsonschema.Reflector{
		DoNotReference: true,
		BaseSchemaID:   jsonschema.ID(schemaURL),
		Mapper: func(t reflect.Type) *jsonschema.Schema {
			if t == reflect.TypeFor[manifest.Requirement]() {
				return requirementSchema
			}
			return nil
		},
	}
	schema := r.Reflect(&manifest.Document{})
	schema.Version = jsonschema.Version
	schema.ID = jsonschema.ID(schemaURL)
	schema.Title = "Lucy manifest"
	schema.Description = "Desired-state manifest (lucy.yaml) for a Lucy-managed Minecraft server."

	raw, err := json.Marshal(schema, jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("regenerated", path)
}
