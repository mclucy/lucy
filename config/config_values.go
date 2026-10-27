package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/mclucy/lucy/document"
	"gopkg.in/yaml.v3"
)

// Values describes one configuration layer. A nil scalar inherits from the
// previous layer; an explicit empty Java home requests automatic selection.
type Values struct {
	Java Java `yaml:"java,omitempty" json:"java" description:"Java runtime selection."`

	node *yaml.Node
}

type Java struct {
	Home *string `yaml:"home,omitempty" json:"home" description:"Java home or executable. An empty value lets Lucy choose a runtime." example:"/path/to/java-home"`
}

func decodeValues(reader io.Reader) (*Values, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var node yaml.Node
	if err := decoder.Decode(&node); errors.Is(err, io.EOF) {
		return &Values{node: &yaml.Node{
			Kind: yaml.DocumentNode,
			Content: []*yaml.Node{{
				Kind:        yaml.MappingNode,
				Tag:         "!!map",
				HeadComment: string(data),
			}},
		}}, nil
	} else if err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	var tail yaml.Node
	if err := decoder.Decode(&tail); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("decode config tail: %w", err)
		}
		return nil, fmt.Errorf("config must contain one YAML document")
	}
	if len(node.Content) == 0 {
		return &Values{}, nil
	}
	if err := validateFields(node.Content[0], reflect.TypeFor[Values](), ""); err != nil {
		return nil, err
	}
	values := &Values{node: &node}
	if err := node.Decode(values); err != nil {
		return nil, fmt.Errorf("decode config values: %w", err)
	}
	return values, nil
}

func encodeValues(values *Values) ([]byte, error) {
	value := reflect.ValueOf(values).Elem()
	if values.node == nil || !hasValue(value) {
		return document.RenderYAML(values, document.YAMLOptions{})
	}
	syncNode(values.node.Content[0], value)
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	encodeErr := encoder.Encode(values.node)
	return output.Bytes(), errors.Join(encodeErr, encoder.Close())
}

func (values *Values) PlainBytes() ([]byte, error) {
	return document.RenderYAML(values, document.YAMLOptions{NoComments: true})
}

func schemaField(t reflect.Type, name string) (reflect.StructField, bool) {
	for i := range t.NumField() {
		field := t.Field(i)
		if field.IsExported() && yamlName(field) == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

func yamlName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
	return name
}

func validateFields(node *yaml.Node, t reflect.Type, prefix string) error {
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if node.Tag == "!!null" {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("config %s must be a mapping", strings.TrimSuffix(prefix, "."))
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		field, ok := schemaField(t, key.Value)
		if !ok {
			return fmt.Errorf("unknown config key %q", prefix+key.Value)
		}
		if field.Type.Kind() == reflect.Struct {
			if err := validateFields(value, field.Type, prefix+key.Value+"."); err != nil {
				return err
			}
			continue
		}
		if value.Tag == "!!null" {
			continue
		}
		if value.Kind == yaml.AliasNode {
			value = value.Alias
		}
		if field.Type.Kind() != reflect.Pointer || field.Type.Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported config schema field %q", prefix+key.Value)
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			return fmt.Errorf("config %s must be a string", prefix+key.Value)
		}
	}
	return nil
}

func hasValue(value reflect.Value) bool {
	if value.Kind() == reflect.Pointer {
		return !value.IsNil()
	}
	for i := range value.Type().NumField() {
		field := value.Type().Field(i)
		if field.IsExported() && hasValue(value.FieldByIndex(field.Index)) {
			return true
		}
	}
	return false
}

func fieldValue(values *Values, key string) (reflect.Value, error) {
	value := reflect.ValueOf(values).Elem()
	for part := range strings.SplitSeq(key, ".") {
		if value.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf("unknown config key %q", key)
		}
		field, ok := schemaField(value.Type(), part)
		if !ok {
			return reflect.Value{}, fmt.Errorf("unknown config key %q", key)
		}
		value = value.FieldByIndex(field.Index)
	}
	return value, nil
}

func mergeValues(global, workspace *Values) *Values {
	merged := &Values{}
	mergeFields(reflect.ValueOf(merged).Elem(), reflect.ValueOf(global).Elem(), reflect.ValueOf(workspace).Elem())
	return merged
}

func mergeFields(target, global, workspace reflect.Value) {
	for i := range target.Type().NumField() {
		field := target.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		dst := target.FieldByIndex(field.Index)
		left, right := global.FieldByIndex(field.Index), workspace.FieldByIndex(field.Index)
		if dst.Kind() == reflect.Struct {
			mergeFields(dst, left, right)
			continue
		}
		switch {
		case !right.IsNil():
			dst.Set(right)
		case !left.IsNil():
			dst.Set(left)
		default:
			dst.Set(reflect.New(dst.Type().Elem()))
		}
	}
}
