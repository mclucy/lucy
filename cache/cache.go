package cache

import "sync"

var (
	networkOnce    sync.Once
	networkHandler *handler
)

func Network() *handler {
	networkOnce.Do(func() {
		networkHandler = newHandler("network")
	})
	return networkHandler
}
