package fn

import (
	"reflect"
	"slices"
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

func ForEach[T any](arr []T, fn func(T)) {
	for _, v := range arr {
		fn(v)
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

func Dedupe[T comparable](arr []T) (res []T) {
	set := map[T]struct{}{}
	for _, item := range arr {
		set[item] = struct{}{}
	}
	for item := range set {
		res = append(res, item)
	}
	return res
}

// KeyValue works together with SortAndExtract to sort a slice of Item
// with their corresponding Index.
type KeyValue[T, Ti any] struct {
	Item  T
	Index Ti
}

func SortAndExtract[T, Ti any](
	arr []KeyValue[T, Ti],
	cmp func(a, b KeyValue[T, Ti]) int,
) (res []T) {
	slices.SortFunc(arr, cmp)
	for _, item := range arr {
		res = append(res, item.Item)
	}
	return res
}
