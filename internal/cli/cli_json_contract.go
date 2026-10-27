package cli

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/spf13/cobra"
)

// JSONResult binds the type written by a command to its discoverable contract.
type JSONResult[T any] struct{}

func (JSONResult[T]) Bind(contracts *JSONContracts, cmd *cobra.Command) {
	t := reflect.TypeFor[T]()
	if !slices.Contains(contracts.results[cmd], t) {
		contracts.results[cmd] = append(contracts.results[cmd], t)
	}
}

func (JSONResult[T]) Write(output *Output, value T) error {
	return output.writeJSON(output.cmd.OutOrStdout(), value)
}

// JSONContracts belongs to one command tree. Schemas are generated only on lookup.
type JSONContracts struct {
	results map[*cobra.Command][]reflect.Type
	ignored map[*cobra.Command]bool
	native  map[*cobra.Command]bool
}

func NewJSONContracts() *JSONContracts {
	return &JSONContracts{
		results: make(map[*cobra.Command][]reflect.Type),
		ignored: make(map[*cobra.Command]bool),
		native:  make(map[*cobra.Command]bool),
	}
}

func (c *JSONContracts) Ignore(cmd *cobra.Command) { c.ignored[cmd] = true }

func (c *JSONContracts) Native(cmd *cobra.Command) { c.native[cmd] = true }

func (c *JSONContracts) Ignores(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if c.ignored[current] {
			return true
		}
	}
	return false
}

func (c *JSONContracts) IsNative(cmd *cobra.Command) bool { return c.native[cmd] }

func (c *JSONContracts) Supports(cmd *cobra.Command) bool {
	return len(c.results[cmd]) > 0 || c.IsNative(cmd)
}

func (c *JSONContracts) Paths(root *cobra.Command) []string {
	paths := make([]string, 0)
	prefix := root.Root().Name() + " "
	for cmd := range c.results {
		for ancestor := cmd; ancestor != nil; ancestor = ancestor.Parent() {
			if ancestor == root {
				paths = append(paths, strings.TrimPrefix(cmd.CommandPath(), prefix))
				break
			}
		}
	}
	slices.Sort(paths)
	return paths
}

func (c *JSONContracts) Schema(cmd *cobra.Command) (*jsonschema.Schema, error) {
	types := c.results[cmd]
	if len(types) == 0 {
		return nil, fmt.Errorf("%s does not provide a JSON result schema", cmd.CommandPath())
	}
	schema := reflectJSONSchema(types)
	schema.Title = cmd.CommandPath() + " JSON result"
	return schema, nil
}

func reflectJSONSchema(types []reflect.Type) *jsonschema.Schema {
	reflector := &jsonschema.Reflector{
		Anonymous:        true,
		AdditionalFields: jsonOutputFields,
		Namer: func(t reflect.Type) string {
			if t.Name() == "" {
				return ""
			}
			return strings.ReplaceAll(t.PkgPath(), "/", ".") + "." + t.Name()
		},
	}
	if len(types) == 1 {
		return reflector.ReflectFromType(types[0])
	}
	result := &jsonschema.Schema{
		Version:     jsonschema.Version,
		Definitions: make(jsonschema.Definitions),
		AnyOf:       make([]*jsonschema.Schema, 0, len(types)),
	}
	for _, t := range types {
		variant := reflector.ReflectFromType(t)
		for name, definition := range variant.Definitions {
			result.Definitions[name] = definition
		}
		variant.Version = ""
		variant.ID = ""
		variant.Definitions = nil
		result.AnyOf = append(result.AnyOf, variant)
	}
	return result
}

// The reflector's defaults do not account for nullable pointers or JSON-v2's
// presence rules for numeric and boolean fields tagged with omitempty.
func jsonOutputFields(t reflect.Type) []reflect.StructField {
	fields := make([]reflect.StructField, 0)
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tags := strings.Split(field.Tag.Get("json"), ",")
		if tags[0] == "-" || field.Anonymous && tags[0] == "" {
			continue
		}
		omitEmpty := slices.Contains(tags[1:], "omitempty")
		omitZero := slices.Contains(tags[1:], "omitzero")
		keyword := ""
		switch field.Type.Kind() {
		case reflect.Pointer:
			if !omitEmpty && !omitZero {
				keyword = "nullable"
			}
		case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			if omitEmpty && !omitZero {
				keyword = "required"
			}
		}
		if keyword == "" {
			continue
		}
		old, exists := field.Tag.Lookup("jsonschema")
		updated := keyword
		if old != "" {
			updated = old + "," + keyword
		}
		tag := string(field.Tag)
		if exists {
			tag = strings.Replace(tag, "jsonschema:"+strconv.Quote(old), "jsonschema:"+strconv.Quote(updated), 1)
		} else {
			tag += " jsonschema:" + strconv.Quote(updated)
		}
		field.Tag = reflect.StructTag(tag)
		fields = append(fields, field)
	}
	return fields
}
