package fn

import "io"

// Ternary returns `a` if `v == true`, `b` if `v == false`.
//
// Do not use this in a loop or a performance-critical code path, as it may cause
// unnecessary evaluations of a and b.
func Ternary[T any](v bool, a T, b T) T {
	if v {
		return a
	}
	return b
}

// CloseReader closes a reader and runs failAction() if error occurs. Call this
// with a defer statement.
func CloseReader(reader io.ReadCloser, failAction func(error)) {
	err := reader.Close()
	if err != nil {
		failAction(err)
	}
}
