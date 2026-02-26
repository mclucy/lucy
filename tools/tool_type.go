package tools

import (
	"encoding/json"
)

// SingleOrSlice represents a value that can be encoded as either:
//   - a single element: `T`
//   - or an array: `[]T`
//
// After unmarshalling, both forms are normalized into a slice shape.
// This type is intended for input-schema compatibility at decoding boundaries.
// Business logic should typically consume plain []T.
type SingleOrSlice[T any] []T

func (s *SingleOrSlice[T]) UnmarshalJSON(data []byte) error {
	// Fast-path by JSON shape to avoid ambiguity for permissive T types.
	for _, b := range data {
		switch b {
		case ' ', '\t', '\n', '\r':
			continue
		case '[':
			var multiple []T
			if err := json.Unmarshal(data, &multiple); err != nil {
				return err
			}
			*s = multiple
			return nil
		default:
			var single T
			if err := json.Unmarshal(data, &single); err != nil {
				return err
			}
			*s = []T{single}
			return nil
		}
	}

	// Empty JSON input is treated as an empty slice.
	*s = nil
	return nil
}
