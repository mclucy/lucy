package config

import (
	"reflect"
	"strings"
)

// RedactedValue replaces a secret setting in configuration output.
const RedactedValue = "********"

// Secret reports whether a setting holds a credential that output must mask.
func Secret(key string) bool {
	field, ok := schemaFieldByKey(key)
	return ok && field.Tag.Get("secret") == "true"
}

// Redacted returns value with every secret setting masked, so the output is
// safe to share. A caller that needs one secret reads that setting by its exact
// key instead.
func Redacted(value any) any {
	switch value := value.(type) {
	case *Values:
		if value == nil {
			return value
		}
		copied := *value
		redactSecrets(reflect.ValueOf(&copied).Elem())
		return &copied
	case CurseForge:
		redactSecrets(reflect.ValueOf(&value).Elem())
		return value
	default:
		return value
	}
}

func schemaFieldByKey(key string) (reflect.StructField, bool) {
	if key == "" {
		return reflect.StructField{}, false
	}
	t := reflect.TypeFor[Values]()
	var field reflect.StructField
	for part := range strings.SplitSeq(key, ".") {
		next, ok := schemaField(t, part)
		if !ok {
			return reflect.StructField{}, false
		}
		field, t = next, next.Type
	}
	return field, true
}

// redactSecrets masks the non-empty secret fields reachable from value in
// place. An empty secret keeps its empty value, which selects the fallback.
func redactSecrets(value reflect.Value) {
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return
	}
	t := value.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		child := value.Field(i)
		if field.Tag.Get("secret") == "true" &&
			child.Kind() == reflect.Pointer &&
			child.Type().Elem().Kind() == reflect.String &&
			!child.IsNil() &&
			child.Elem().String() != "" {
			child.Elem().SetString(RedactedValue)
			continue
		}
		redactSecrets(child)
	}
}
