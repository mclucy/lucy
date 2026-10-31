package document

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

const yamlSchemaDirective = "yaml-language-server:"

// YAMLOptions controls generated YAML comments.
type YAMLOptions struct {
	// NoComments writes the document without generated comments.
	NoComments bool

	// SchemaURL adds the editor schema directive as the document header.
	SchemaURL string
}

// Commenter applies the comments declared by serialized types. Descriptions
// and examples belong to the Go types, so no document has a hand-written
// commented template.
type Commenter struct {
	options YAMLOptions
}

// NewCommenter configures a commenter for one document family.
func NewCommenter(options YAMLOptions) Commenter {
	return Commenter{options: options}
}

// Render writes value as a fresh YAML document.
func (c Commenter) Render(value any) ([]byte, error) {
	node, err := c.node(value, true)
	if err != nil {
		return nil, err
	}
	return c.marshal(node)
}

// Merge writes value into existing. Comments already present in existing are
// never replaced: new nodes adopt the comments declared by value's type, and
// blank comment slots on existing nodes are filled. A changed description
// therefore reaches new documents and new fields without rewriting the
// comments a user already has.
func (c Commenter) Merge(existing []byte, value any) ([]byte, error) {
	incoming, err := c.node(value, false)
	if err != nil {
		return nil, err
	}
	var target yaml.Node
	if err := yaml.Unmarshal(existing, &target); err != nil {
		return nil, fmt.Errorf("decode existing yaml: %w", err)
	}
	root := documentRoot(&target)
	if root == nil {
		header := strings.TrimSpace(string(existing))
		if header == "" {
			return c.marshal(incoming)
		}
		// A document that held only comments keeps them as a document header,
		// independent of the entries written below.
		document := &yaml.Node{
			Kind:        yaml.DocumentNode,
			HeadComment: header,
			Content:     []*yaml.Node{incoming},
		}
		return c.marshal(document)
	}
	mergeYAMLNode(root, incoming)
	c.removeSchemaDirective(&target)
	return c.marshal(&target)
}

// node encodes value and documents its fields. examples selects the template
// form, which also shows omitted fields as commented examples; a merge into an
// existing document documents only the fields it writes.
func (c Commenter) node(value any, examples bool) (*yaml.Node, error) {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return nil, err
	}
	if c.options.NoComments {
		return &node, nil
	}
	t := reflect.TypeOf(value)
	if t == nil {
		return &node, nil
	}
	if err := validateYAMLDescriptions(t, map[reflect.Type]bool{}); err != nil {
		return nil, err
	}
	if err := describeYAML(&node, t, examples); err != nil {
		return nil, err
	}
	return &node, nil
}

func (c Commenter) marshal(node *yaml.Node) ([]byte, error) {
	if node.Kind == yaml.MappingNode && len(node.Content) == 0 && node.FootComment != "" {
		return commentBlock(node.FootComment), nil
	}
	data, err := marshalYAMLNode(node)
	if err != nil {
		return nil, err
	}
	if c.options.NoComments || c.options.SchemaURL == "" {
		return data, nil
	}
	return append([]byte("# yaml-language-server: $schema="+c.options.SchemaURL+"\n"), data...), nil
}

// commentBlock renders a document that contains only generated examples.
func commentBlock(comment string) []byte {
	var output strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(comment, "\n"), "\n") {
		if !strings.HasPrefix(line, "#") {
			output.WriteString("# ")
		}
		output.WriteString(line)
		output.WriteByte('\n')
	}
	return []byte(output.String())
}

// removeSchemaDirective drops the schema header the previous write added, so
// the next write can place the canonical one. User comments around it stay.
func (c Commenter) removeSchemaDirective(document *yaml.Node) {
	if c.options.SchemaURL == "" {
		return
	}
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		node.HeadComment = withoutDirective(node.HeadComment)
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(document)
}

func withoutDirective(comment string) string {
	if comment == "" {
		return ""
	}
	kept := make([]string, 0, strings.Count(comment, "\n")+1)
	for line := range strings.SplitSeq(comment, "\n") {
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if strings.HasPrefix(text, yamlSchemaDirective) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Trim(strings.Join(kept, "\n"), "\n")
}

func documentRoot(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		return node.Content[0]
	}
	if node.Kind == 0 {
		return nil
	}
	return node
}

// mergeYAMLNode rewrites existing from incoming. Existing comments always win;
// incoming comments only fill empty slots or accompany new nodes.
func mergeYAMLNode(existing, incoming *yaml.Node) {
	fillComments(existing, incoming)
	if existing.Kind != incoming.Kind || existing.Tag != incoming.Tag {
		replaceNode(existing, incoming)
		return
	}
	switch existing.Kind {
	case yaml.DocumentNode:
		if len(existing.Content) == 0 || len(incoming.Content) == 0 {
			replaceNode(existing, incoming)
			return
		}
		mergeYAMLNode(existing.Content[0], incoming.Content[0])
	case yaml.MappingNode:
		if len(existing.Content) == 0 {
			// An empty flow mapping such as {} carries no layout to preserve.
			existing.Style = incoming.Style
		}
		mergeMappingNode(existing, incoming)
	case yaml.SequenceNode:
		if len(existing.Content) == 0 {
			existing.Style = incoming.Style
		}
		mergeSequenceNode(existing, incoming)
	case yaml.ScalarNode:
		existing.Value = incoming.Value
		existing.Style = incoming.Style
	case yaml.AliasNode:
		existing.Alias = incoming.Alias
		existing.Value = incoming.Value
	}
}

// mergeMappingNode keeps the existing entry order and appends keys the
// document did not have. Entries absent from incoming are removed.
func mergeMappingNode(existing, incoming *yaml.Node) {
	pending := make(map[string]int, len(incoming.Content)/2)
	for i := 0; i+1 < len(incoming.Content); i += 2 {
		pending[incoming.Content[i].Value] = i
	}
	kept := make([]*yaml.Node, 0, len(existing.Content))
	for i := 0; i+1 < len(existing.Content); i += 2 {
		key := existing.Content[i]
		position, ok := pending[key.Value]
		if !ok {
			continue
		}
		mergeYAMLNode(key, incoming.Content[position])
		mergeYAMLNode(existing.Content[i+1], incoming.Content[position+1])
		kept = append(kept, key, existing.Content[i+1])
		delete(pending, key.Value)
	}
	for i := 0; i+1 < len(incoming.Content); i += 2 {
		if _, ok := pending[incoming.Content[i].Value]; !ok {
			continue
		}
		kept = append(kept, cloneYAMLNode(incoming.Content[i]), cloneYAMLNode(incoming.Content[i+1]))
	}
	existing.Content = kept
}

// mergeSequenceNode merges entries by a stable path key when both sequences
// have one, and by position otherwise. Declarations such as tracked files
// keep their comments when an entry is inserted or removed.
func mergeSequenceNode(existing, incoming *yaml.Node) {
	if mergeSequenceByPath(existing, incoming) {
		return
	}
	count := min(len(existing.Content), len(incoming.Content))
	for i := range count {
		mergeYAMLNode(existing.Content[i], incoming.Content[i])
	}
	if len(incoming.Content) > len(existing.Content) {
		for _, node := range incoming.Content[count:] {
			existing.Content = append(existing.Content, cloneYAMLNode(node))
		}
		return
	}
	existing.Content = existing.Content[:count]
}

// mergeSequenceByPath reports whether it merged the sequences. It requires
// every entry on both sides to be a mapping with a unique, non-empty string
// path, which is the identity tracked files and lock entries declare.
func mergeSequenceByPath(existing, incoming *yaml.Node) bool {
	if _, ok := sequencePathIndex(existing); !ok {
		return false
	}
	incomingIndex, ok := sequencePathIndex(incoming)
	if !ok {
		return false
	}
	kept := make([]*yaml.Node, 0, len(existing.Content))
	for _, entry := range existing.Content {
		path, _ := sequenceEntryPath(entry)
		position, ok := incomingIndex[path]
		if !ok {
			continue
		}
		mergeYAMLNode(entry, incoming.Content[position])
		kept = append(kept, entry)
		delete(incomingIndex, path)
	}
	for _, entry := range incoming.Content {
		path, _ := sequenceEntryPath(entry)
		if _, ok := incomingIndex[path]; !ok {
			continue
		}
		kept = append(kept, cloneYAMLNode(entry))
	}
	existing.Content = kept
	return true
}

func sequencePathIndex(node *yaml.Node) (map[string]int, bool) {
	if node.Kind != yaml.SequenceNode {
		return nil, false
	}
	index := make(map[string]int, len(node.Content))
	for i, entry := range node.Content {
		path, ok := sequenceEntryPath(entry)
		if !ok {
			return nil, false
		}
		if _, exists := index[path]; exists {
			return nil, false
		}
		index[path] = i
	}
	return index, true
}

func sequenceEntryPath(entry *yaml.Node) (string, bool) {
	if entry.Kind != yaml.MappingNode {
		return "", false
	}
	for i := 0; i+1 < len(entry.Content); i += 2 {
		key, value := entry.Content[i], entry.Content[i+1]
		if key.Value != "path" {
			continue
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || value.Value == "" {
			return "", false
		}
		return value.Value, true
	}
	return "", false
}

func fillComments(existing, incoming *yaml.Node) {
	if existing.HeadComment == "" {
		existing.HeadComment = incoming.HeadComment
	}
	if existing.LineComment == "" {
		existing.LineComment = incoming.LineComment
	}
	if existing.FootComment == "" {
		existing.FootComment = incoming.FootComment
	}
}

// replaceNode adopts incoming's shape while keeping every comment existing
// carried.
func replaceNode(existing, incoming *yaml.Node) {
	head, line, foot := existing.HeadComment, existing.LineComment, existing.FootComment
	*existing = *cloneYAMLNode(incoming)
	if head != "" {
		existing.HeadComment = head
	}
	if line != "" {
		existing.LineComment = line
	}
	if foot != "" {
		existing.FootComment = foot
	}
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	copy := *node
	copy.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		copy.Content[i] = cloneYAMLNode(child)
	}
	return &copy
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

func describeYAML(node *yaml.Node, t reflect.Type, examples bool) error {
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
				if !examples {
					continue
				}
				example, err := yamlFieldExample(field)
				if err != nil {
					return err
				}
				if example == nil {
					continue
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
			if err := describeYAML(child, field.Type, examples); err != nil {
				return err
			}
		}
		if pending.Len() != 0 {
			if len(node.Content) == 0 {
				node.FootComment = strings.TrimRight(pending.String(), "\n")
			} else {
				// A mapping's own foot comment escapes the indentation of its
				// fields, while a key's foot comment stays in the mapping.
				key := node.Content[len(node.Content)-2]
				key.FootComment = strings.TrimRight(pending.String(), "\n")
			}
		}
	case reflect.Map:
		if node.Kind == yaml.MappingNode {
			for i := 1; i < len(node.Content); i += 2 {
				if err := describeYAML(node.Content[i], t.Elem(), examples); err != nil {
					return err
				}
			}
		}
	case reflect.Slice, reflect.Array:
		if node.Kind == yaml.SequenceNode {
			for _, child := range node.Content {
				if err := describeYAML(child, t.Elem(), examples); err != nil {
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

// yamlFieldExample builds the commented example a template shows for an
// omitted field. An example tag of "-" suppresses that example, so the field
// appears only once the document carries it.
func yamlFieldExample(field reflect.StructField) (*yaml.Node, error) {
	if example, ok := field.Tag.Lookup("example"); ok {
		if example == "-" {
			return nil, nil
		}
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
			if example == nil {
				continue
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
