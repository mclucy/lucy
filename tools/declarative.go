package tools

import (
	"reflect"
)

// Exists checks if an element exists in a slice. It returns true if the element
// is found, and false otherwise.
func Exists[T comparable](arr []T, elem T) bool {
	for _, v := range arr {
		if v == elem {
			return true
		}
	}
	return false
}

func Count[T comparable](arr []T, elem T) int {
	count := 0
	for _, v := range arr {
		if v == elem {
			count++
		}
	}
	return count
}

func ForEach[T any](arr []T, fn func(T)) {
	for _, v := range arr {
		fn(v)
	}
}

func ForEachOnMatrix[T any](mat [][]T, fn func(T)) {
	for _, row := range mat {
		for _, v := range row {
			fn(v)
		}
	}
}

func ForEachRecursive[T any](arr []any, fn func(T)) {
	if arr == nil {
		return
	}
	eType := reflect.TypeOf(fn).In(0)

	for _, v := range arr {
		if v == nil {
			continue
		}

		val := reflect.ValueOf(v)

		// Recursively handle slices
		if val.Kind() == reflect.Slice {
			sliceAny := make([]any, val.Len())
			for i := 0; i < val.Len(); i++ {
				sliceAny[i] = val.Index(i).Interface()
			}
			ForEachRecursive(sliceAny, fn)
			continue
		}

		// Call the function if types match
		if val.Type().AssignableTo(eType) {
			fn(v.(T))
		}
	}
}

func IsEmptyVector[T any](arr []T) bool {
	if len(arr) == 0 {
		return true
	}
	for _, e := range arr {
		if !isEmptyVectorValue(reflect.ValueOf(e)) {
			return false
		}
	}
	return true
}

func isEmptyVectorValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}
	if v.Kind() != reflect.Slice {
		return false
	}
	if v.Len() == 0 {
		return true
	}
	for i := 0; i < v.Len(); i++ {
		if !isEmptyVectorValue(v.Index(i)) {
			return false
		}
	}
	return true
}
