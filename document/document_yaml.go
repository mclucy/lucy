package document

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

type YAMLOptions struct {
	NoComments bool
}

// RenderYAML documents active fields and shows omitted fields as commented
// examples. Field descriptions and examples belong to the serialized types.
func RenderYAML(value any, options YAMLOptions) ([]byte, error) {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return nil, err
	}
	if options.NoComments {
		stripComments(&node)
		return marshalYAMLNode(&node)
	}
	t := reflect.TypeOf(value)
	if t == nil {
		return marshalYAMLNode(&node)
	}
	if err := validateYAMLDescriptions(t, map[reflect.Type]bool{}); err != nil {
		return nil, err
	}
	if err := describeYAML(&node, t); err != nil {
		return nil, err
	}
	if node.Kind == yaml.MappingNode && len(node.Content) == 0 && node.FootComment != "" {
		var output strings.Builder
		for line := range strings.SplitSeq(strings.TrimRight(node.FootComment, "\n"), "\n") {
			if !strings.HasPrefix(line, "#") {
				output.WriteString("# ")
			}
			output.WriteString(line)
			output.WriteByte('\n')
		}
		return []byte(output.String()), nil
	}
	return marshalYAMLNode(&node)
}

func marshalYAMLNode(node *yaml.Node) ([]byte, error) {
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	encodeErr := encoder.Encode(node)
	return output.Bytes(), errors.Join(encodeErr, encoder.Close())
}

func yamlFieldName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
	return name
}

func validateYAMLDescriptions(t reflect.Type, seen map[reflect.Type]bool) error {
	if t == nil || seen[t] {
		return nil
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return validateYAMLDescriptions(t.Elem(), seen)
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() || yamlFieldName(field) == "-" {
				continue
			}
			if yamlFieldName(field) == "" || field.Tag.Get("description") == "" {
				return fmt.Errorf("yaml field %s.%s requires a name and description", t.Name(), field.Name)
			}
			if err := validateYAMLDescriptions(field.Type, seen); err != nil {
				return err
			}
		}
	}
	return nil
}

func describeYAML(node *yaml.Node, t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return nil // Custom marshalers can select a scalar shorthand.
		}
		var pending strings.Builder
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() || yamlFieldName(field) == "-" {
				continue
			}
			name := yamlFieldName(field)
			index := yamlKeyIndex(node, name)
			if index < 0 {
				example, err := yamlFieldExample(field)
				if err != nil {
					return err
				}
				mapping := &yaml.Node{
					Kind:    yaml.MappingNode,
					Content: []*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, example},
				}
				data, err := marshalYAMLNode(mapping)
				if err != nil {
					return err
				}
				pending.WriteString(field.Tag.Get("description") + "\n")
				pending.Write(data)
				continue
			}
			key, child := node.Content[index], node.Content[index+1]
			key.HeadComment = pending.String() + field.Tag.Get("description")
			pending.Reset()
			if err := describeYAML(child, field.Type); err != nil {
				return err
			}
		}
		if pending.Len() != 0 {
			if len(node.Content) == 0 {
				node.FootComment = strings.TrimRight(pending.String(), "\n")
			} else {
				// Mapping foot comments escape the indentation of their fields.
				key := node.Content[len(node.Content)-2]
				key.HeadComment = pending.String() + key.HeadComment
			}
		}
	case reflect.Map:
		if node.Kind == yaml.MappingNode {
			for i := 1; i < len(node.Content); i += 2 {
				if err := describeYAML(node.Content[i], t.Elem()); err != nil {
					return err
				}
			}
		}
	case reflect.Slice, reflect.Array:
		if node.Kind == yaml.SequenceNode {
			for _, child := range node.Content {
				if err := describeYAML(child, t.Elem()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func yamlKeyIndex(node *yaml.Node, name string) int {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return i
		}
	}
	return -1
}

func yamlFieldExample(field reflect.StructField) (*yaml.Node, error) {
	if example, ok := field.Tag.Lookup("example"); ok {
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(example), &node); err != nil {
			return nil, fmt.Errorf("yaml example for %s: %w", field.Name, err)
		}
		if len(node.Content) != 0 {
			return node.Content[0], nil
		}
	}
	t := field.Type
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	node := &yaml.Node{}
	switch t.Kind() {
	case reflect.Struct:
		node.Kind = yaml.MappingNode
		for i := range t.NumField() {
			child := t.Field(i)
			if !child.IsExported() || yamlFieldName(child) == "-" {
				continue
			}
			example, err := yamlFieldExample(child)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, &yaml.Node{
				Kind:        yaml.ScalarNode,
				Tag:         "!!str",
				Value:       yamlFieldName(child),
				HeadComment: child.Tag.Get("description"),
			}, example)
		}
	case reflect.Map:
		node.Kind = yaml.MappingNode
	case reflect.Slice, reflect.Array:
		node.Kind = yaml.SequenceNode
	default:
		if err := node.Encode(reflect.Zero(t).Interface()); err != nil {
			return nil, err
		}
	}
	return node, nil
}

func stripComments(node *yaml.Node) {
	node.HeadComment = ""
	node.LineComment = ""
	node.FootComment = ""
	for _, child := range node.Content {
		stripComments(child)
	}
}
