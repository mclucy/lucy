package lucyerror

import "errors"

var NoLucyError = errors.New("lucy is not installed, run `lucy init` before downloading mods")

var (
	InvalidPlatformError    = errors.New("invalid platform")
	PackageSyntaxError      = errors.New("invalid package syntax")
	EmptyPackageSyntaxError = errors.New("empty package string")
)
