package fn

import (
	"io"
	"sync"
)

// TernaryFunc gives a if expr == true, b if expr == false. For a simple
// bool expression, use Ternary instead.
func TernaryFunc[T any](expr func() bool, a T, b T) T {
	if expr() {
		return a
	}
	return b
}

// Ternary returns a if v == true, b if v == false. For a function parameter, use
// TernaryFunc instead.
//
// Do not use this in a loop or a performance-critical code path, as it may cause
// unnecessary evaluations of a and b.
func Ternary[T any](v bool, a T, b T) T {
	if v {
		return a
	}
	return b
}

func TernaryLazy[T any](v bool, a func() T, b func() T) T {
	if v {
		return a()
	}
	return b()
}

// Memoize is only used for functions that do not take any arguments and return
// a value (typically a struct) that can be treated as a constant.
func Memoize[T any](f func() T) func() T {
	var res T
	var once sync.Once
	return func() T {
		once.Do(
			func() {
				res = f()
			},
		)
		return res
	}
}

// Insert inserts a value into a slice at a slice[pos]. If the pos is out of
// bounds, the slice remains unchanged.
func Insert[T any](slice []T, pos int, value ...T) []T {
	if pos < 0 || pos > len(slice) {
		return slice
	}
	return append(slice[:pos], append(value, slice[pos:]...)...)
}

// CloseReader closes a reader and runs failAction() if error occurs. Call this
// with a defer statement.
func CloseReader(reader io.ReadCloser, failAction func(error)) {
	err := reader.Close()
	if err != nil {
		failAction(err)
	}
}

// Decorate applies a series of decorators to a function. This is used to
// prevent nested function calls for better readability.
func Decorate[T interface{}](f T, decorators ...func(T) T) T {
	for _, decorator := range decorators {
		f = decorator(f)
	}
	return f
}

func Compose[Tx any, Ty any, Tz any](x func(Tx) Ty, y func(Ty) Tz) func(Tx) Tz {
	return func(t Tx) Tz {
		return y(x(t))
	}
}
