package main

import (
	stdjson "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"reflect"

	"github.com/invopop/jsonschema"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
	orderedmap "github.com/pb33f/ordered-map/v2"
)

const schemaURL = "https://raw.githubusercontent.com/mclucy/lucy/main/manifest/manifest.schema.json"

func main() {
	path := "manifest/manifest.schema.json"

	requirementSchema := &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			{
				Type:        "string",
				Description: "A version selector: any, stable, beta, a tag, or an exact version.",
			},
			func() *jsonschema.Schema {
				selector := &jsonschema.Schema{
					Type:        "string",
					Description: "A version selector: any, stable, beta, a tag, or an exact version.",
				}
				filePattern := &jsonschema.Schema{
					Type:        "string",
					Description: "Glob pattern matching the upstream artifact filename.",
				}
				enabled := &jsonschema.Schema{
					Type:        "boolean",
					Default:     true,
					Description: "Whether the requirement is active in the resolved environment.",
				}
				properties := orderedmap.New[string, *jsonschema.Schema]()
				properties.Set("selector", selector)
				properties.Set("file_pattern", filePattern)
				properties.Set("enabled", enabled)
				return &jsonschema.Schema{
					Type:                 "object",
					Properties:           properties,
					AdditionalProperties: jsonschema.FalseSchema,
				}
			}(),
		},
	}

	trackedFileSchema := func() *jsonschema.Schema {
		path := &jsonschema.Schema{
			Type:        "string",
			Description: "Workspace-relative forward-slash path to the tracked file.",
		}
		immutable := &jsonschema.Schema{
			Type:        "boolean",
			Default:     false,
			Description: "Whether deviation from the accepted digest produces an immutable-file warning.",
		}
		hash := &jsonschema.Schema{
			Type:        "string",
			Pattern:     `^sha256:[0-9a-f]{64}$`,
			Description: "Accepted SHA-256 digest in canonical sha256:<hex> form.",
		}
		recoverURL := &jsonschema.Schema{
			Type:        "string",
			Format:      "uri",
			Pattern:     `^https?://`,
			Description: "Remote HTTP(S) URL used to restore missing file contents.",
		}
		properties := orderedmap.New[string, *jsonschema.Schema]()
		properties.Set("path", path)
		properties.Set("immutable", immutable)
		properties.Set("hash", hash)
		properties.Set("recover_url", recoverURL)
		return &jsonschema.Schema{
			Type:                 "object",
			Properties:           properties,
			Required:             []string{"path"},
			AdditionalProperties: jsonschema.FalseSchema,
			AllOf: []*jsonschema.Schema{
				{
					If: &jsonschema.Schema{
						Properties: func() *orderedmap.OrderedMap[string, *jsonschema.Schema] {
							p := orderedmap.New[string, *jsonschema.Schema]()
							p.Set("immutable", &jsonschema.Schema{Const: true})
							return p
						}(),
						Required: []string{"immutable"},
					},
					Then: &jsonschema.Schema{
						Required: []string{"hash"},
					},
				},
				{
					If: &jsonschema.Schema{
						Required: []string{"recover_url"},
					},
					Then: &jsonschema.Schema{
						Required: []string{"hash"},
					},
				},
			},
		}
	}()

	javaSchema := func() *jsonschema.Schema {
		major := func() *jsonschema.Schema {
			return &jsonschema.Schema{
				Type:    "integer",
				Minimum: stdjson.Number(fmt.Sprint(manifest.MinimumJava)),
			}
		}
		version := &jsonschema.Schema{
			OneOf: []*jsonschema.Schema{
				major(),
				{Const: manifest.JavaAuto},
			},
			Description: "The Java major version, or auto.",
		}
		distributions := []any{manifest.JavaAuto, "any"}
		for _, distribution := range types.JavaDistributions {
			distributions = append(distributions, string(distribution))
		}
		distribution := &jsonschema.Schema{
			Enum:        distributions,
			Description: "The Java distribution. Omit it to accept any distribution.",
		}
		properties := orderedmap.New[string, *jsonschema.Schema]()
		properties.Set("version", version)
		properties.Set("distribution", distribution)
		short := major()
		short.Description = "The Java major version."
		return &jsonschema.Schema{
			OneOf: []*jsonschema.Schema{
				short,
				{
					Const:       manifest.JavaAuto,
					Description: "Follow the Java version Mojang recommends.",
				},
				{
					Type:                 "object",
					Properties:           properties,
					AdditionalProperties: jsonschema.FalseSchema,
				},
			},
		}
	}()

	r := &jsonschema.Reflector{
		DoNotReference: true,
		BaseSchemaID:   jsonschema.ID(schemaURL),
		Mapper: func(t reflect.Type) *jsonschema.Schema {
			switch t {
			case reflect.TypeFor[manifest.Requirement]():
				return requirementSchema
			case reflect.TypeFor[manifest.Java]():
				return javaSchema
			case reflect.TypeFor[types.TrackedFile]():
				return trackedFileSchema
			default:
				return nil
			}
		},
	}
	schema := r.Reflect(&manifest.Document{})
	schema.Version = jsonschema.Version
	schema.ID = schemaURL
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
