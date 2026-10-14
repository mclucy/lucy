// Package cache owns request metadata and expiration policy over reusable CAS storage.
// Its global objects have a separate lifecycle from project-local backups.
package cache

import "sync"

var networkHandler = sync.OnceValue(func() *handler {
	return newHandler("network", DefaultCacheConfig())
})

func Network() *handler {
	return networkHandler()
}
