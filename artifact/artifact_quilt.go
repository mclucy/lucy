package artifact

import (
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/version"
)

const quiltModPath = "quilt.mod.json"

// validateQuiltDescriptor rejects schema versions this reader cannot
// interpret instead of guessing at a newer layout.
func validateQuiltDescriptor(descriptor *quiltModFile) error {
	if descriptor.SchemaVersion != 1 {
		return fmt.Errorf(
			"unsupported Quilt schema_version %d",
			descriptor.SchemaVersion,
		)
	}
	if descriptor.Loader.ID == "" {
		return fmt.Errorf("Quilt descriptor is missing quilt_loader.id")
	}
	return nil
}

// quiltModFile is the schema_version 1 quilt.mod.json document. The schema
// keeps dependency, license, and contributor values in several shapes, so the
// flexible fields decode into any and are interpreted through the helpers in
// this file.
type quiltModFile struct {
	SchemaVersion int         `json:"schema_version"`
	Loader        quiltLoader `json:"quilt_loader"`
	Minecraft     struct {
		Environment string `json:"environment"`
	} `json:"minecraft"`
}

type quiltLoader struct {
	Group    string                 `json:"group"`
	ID       string                 `json:"id"`
	Version  string                 `json:"version"`
	Provides []quiltProvided        `json:"provides"`
	Jars     []string               `json:"jars"`
	Depends  []quiltDependencyEntry `json:"depends"`
	Breaks   []quiltDependencyEntry `json:"breaks"`
	Metadata *quiltMetadata         `json:"metadata"`
}

type quiltMetadata struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Contributors map[string]any    `json:"contributors"`
	Contact      map[string]string `json:"contact"`
	License      quiltLicense      `json:"license"`
}

// quiltProvided accepts the string shorthand and the object form of a
// provides entry. Version is empty when the entry omits it, which means the
// providing module's own version applies.
type quiltProvided struct {
	ID      string
	Version version.Version
}

func (p *quiltProvided) UnmarshalJSON(data []byte) error {
	var id string
	if err := json.Unmarshal(data, &id); err == nil {
		p.ID = id
		return nil
	}
	var object struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("invalid Quilt provides entry: %w", err)
	}
	if object.ID == "" {
		return fmt.Errorf("Quilt provides entry is missing an id")
	}
	p.ID = object.ID
	p.Version = version.Version(object.Version)
	return nil
}

// quiltLicense collapses the string, array, and object license forms into the
// single display string catalog.Metadata carries.
type quiltLicense string

func (l *quiltLicense) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*l = quiltLicense(single)
		return nil
	}
	var many []quiltLicense
	if err := json.Unmarshal(data, &many); err == nil {
		parts := make([]string, 0, len(many))
		for _, license := range many {
			if license != "" {
				parts = append(parts, string(license))
			}
		}
		*l = quiltLicense(strings.Join(parts, ", "))
		return nil
	}
	var object struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("invalid Quilt license: %w", err)
	}
	switch {
	case object.ID != "":
		*l = quiltLicense(object.ID)
	case object.Name != "":
		*l = quiltLicense(object.Name)
	}
	return nil
}

// quiltDependencyEntry models one element of a Quilt depends or breaks array.
// An element is a mod id string, a dependency object, or an array of
// dependency objects, and the two latter shapes may nest.
type quiltDependencyEntry struct {
	ID       string
	Versions any
	Optional bool
	// Environment is the Minecraft game provider option. A dependency that
	// applies only to the physical client does not constrain a server.
	Environment string
	Unless      *quiltDependencyEntry
	Elements    []quiltDependencyEntry
	array       bool
}

func (e *quiltDependencyEntry) UnmarshalJSON(data []byte) error {
	var id string
	if err := json.Unmarshal(data, &id); err == nil {
		e.ID = id
		return nil
	}
	var elements []quiltDependencyEntry
	if err := json.Unmarshal(data, &elements); err == nil {
		e.Elements = elements
		e.array = true
		return nil
	}
	var object struct {
		ID          string                `json:"id"`
		Versions    any                   `json:"versions"`
		Optional    bool                  `json:"optional"`
		Environment string                `json:"environment"`
		Unless      *quiltDependencyEntry `json:"unless"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("invalid Quilt dependency entry: %w", err)
	}
	if object.ID == "" {
		return fmt.Errorf("Quilt dependency object is missing an id")
	}
	e.ID = object.ID
	e.Versions = object.Versions
	e.Optional = object.Optional
	e.Environment = object.Environment
	e.Unless = object.Unless
	return nil
}

// alternatives flattens nested dependency arrays into the alternatives their
// enclosing entry offers. depends arrays match when any alternative matches.
func (e quiltDependencyEntry) alternatives() []quiltDependencyEntry {
	if !e.array {
		return []quiltDependencyEntry{e}
	}
	var alternatives []quiltDependencyEntry
	for _, element := range e.Elements {
		alternatives = append(alternatives, element.alternatives()...)
	}
	return alternatives
}

// serverAlternatives drops alternatives that apply only to the physical
// client. The Minecraft game provider adds the environment option, and a
// dedicated server does not enforce client-only dependencies.
func (e quiltDependencyEntry) serverAlternatives() []quiltDependencyEntry {
	alternatives := e.alternatives()
	applicable := alternatives[:0]
	for _, alternative := range alternatives {
		if quiltDependencyAppliesToServer(alternative) {
			applicable = append(applicable, alternative)
		}
	}
	return applicable
}

func quiltDependencyAppliesToServer(entry quiltDependencyEntry) bool {
	switch strings.ToLower(strings.TrimSpace(entry.Environment)) {
	case "", "*", "any", "both", "server", "dedicated_server":
		return true
	default:
		return false
	}
}

// quiltVersionNode converts one versions value into a descriptor dependency
// node. A plain string stays verbatim, because the module carries the Quilt
// dialect. The deprecated array and the any form are alternatives, and the all
// form is a conjunction.
func quiltVersionNode(
	raw any,
	id string,
	kind catalog.DependencyKind,
) (catalog.DescriptorDependency, error) {
	node := catalog.DescriptorDependency{ID: id, Kind: kind}
	switch value := raw.(type) {
	case nil:
		return node, nil
	case string:
		node.Constraint = value
		return node, nil
	case []any:
		return quiltAnyNode(value, id, kind)
	case map[string]any:
		if len(value) != 1 {
			return catalog.DescriptorDependency{}, fmt.Errorf(
				"Quilt versions object must hold exactly one of any or all",
			)
		}
		if anyRaw, ok := value["any"]; ok {
			return quiltAnyNode(quiltVersionItems(anyRaw), id, kind)
		}
		if allRaw, ok := value["all"]; ok {
			return quiltAllNode(quiltVersionItems(allRaw), id, kind)
		}
		return catalog.DescriptorDependency{}, fmt.Errorf(
			"Quilt versions object must hold exactly one of any or all",
		)
	default:
		return catalog.DescriptorDependency{}, fmt.Errorf(
			"invalid Quilt versions value %T",
			raw,
		)
	}
}

func quiltVersionItems(raw any) []any {
	if items, ok := raw.([]any); ok {
		return items
	}
	return []any{raw}
}

func quiltAnyNode(
	items []any,
	id string,
	kind catalog.DependencyKind,
) (catalog.DescriptorDependency, error) {
	node := catalog.DescriptorDependency{ID: id, Kind: kind}
	texts := make([]string, 0, len(items))
	plain := true
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			plain = false
			break
		}
		texts = append(texts, text)
	}
	if plain {
		node.Constraint = strings.Join(texts, " || ")
		return node, nil
	}
	if len(items) == 0 {
		return node, nil
	}

	alternatives := make([]catalog.DescriptorDependency, 0, len(items))
	for _, item := range items {
		alternative, err := quiltVersionNode(item, id, kind)
		if err != nil {
			return catalog.DescriptorDependency{}, err
		}
		alternatives = append(alternatives, alternative)
	}
	// The node's own leaf is the first alternative, so its constraint applies
	// when the module is present.
	node = alternatives[0]
	node.AnyOf = append(node.AnyOf, alternatives[1:]...)
	return node, nil
}

func quiltAllNode(
	items []any,
	id string,
	kind catalog.DependencyKind,
) (catalog.DescriptorDependency, error) {
	node := catalog.DescriptorDependency{ID: id, Kind: kind}
	node.AllOf = make([]catalog.DescriptorDependency, 0, len(items))
	for _, item := range items {
		condition, err := quiltVersionNode(item, id, kind)
		if err != nil {
			return catalog.DescriptorDependency{}, err
		}
		node.AllOf = append(node.AllOf, condition)
	}
	return node, nil
}

func quiltRole(value any) string {
	switch roles := value.(type) {
	case string:
		return roles
	case []any:
		parts := make([]string, 0, len(roles))
		for _, role := range roles {
			if text, ok := role.(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}
