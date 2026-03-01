package cache

var (
	// Network is the cache handler instance for http requests. It is indexed
	// by the URL of the request.
	Network = newHandler("network")
	// Package is the cache handler instance for package files. It is indexed
	// by the package ID.
	Package = newHandler("package")
)
