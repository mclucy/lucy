package config

import (
	"reflect"
	"slices"

	"gopkg.in/yaml.v3"
)

func syncNode(node *yaml.Node, value reflect.Value) {
	if node.Kind == yaml.AliasNode {
		*node = *cloneNode(node.Alias)
	}
	if node.Kind != yaml.MappingNode {
		node.Kind = yaml.MappingNode
		node.Tag = "!!map"
		node.Style = 0
		node.Value = ""
		node.Content = nil
		node.Alias = nil
	}
	for i := range value.Type().NumField() {
		field := value.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		current := value.FieldByIndex(field.Index)
		name := yamlName(field)
		index := nodeIndex(node, name)
		if !hasValue(current) {
			if index >= 0 {
				node.Content = slices.Delete(node.Content, index, index+2)
			}
			continue
		}
		if index < 0 {
			index = len(node.Content)
			node.Content = append(node.Content,
				&yaml.Node{
					Kind:        yaml.ScalarNode,
					Tag:         "!!str",
					Value:       name,
					HeadComment: field.Tag.Get("description"),
				},
				&yaml.Node{},
			)
		}
		child := node.Content[index+1]
		if current.Kind() == reflect.Struct {
			syncNode(child, current)
			continue
		}
		child.Kind = yaml.ScalarNode
		child.Tag = "!!str"
		child.Value = current.Elem().String()
		child.Content = nil
		child.Alias = nil
	}
}

func nodeIndex(node *yaml.Node, name string) int {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return i
		}
	}
	return -1
}

func cloneNode(node *yaml.Node) *yaml.Node {
	copy := *node
	copy.Anchor = ""
	copy.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		copy.Content[i] = cloneNode(child)
	}
	return &copy
}
