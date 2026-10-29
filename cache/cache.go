// Package cache owns request metadata and expiration policy over reusable CAS
// storage. Its objects have a separate lifecycle from project-local backups.
package cache

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/mclucy/lucy/log"
)

// ErrNoHTTPClient reports a cache client constructed without a transport.
var ErrNoHTTPClient = errors.New("cache client has no http client")

// DefaultRoot reports the per-user directory the network cache lives in. It is
// a path helper for the construction boundary, not a cache instance.
func DefaultRoot() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "lucy", "network")
}

// Client is one configured network cache: a content-addressed store, its
// index and expiration policy, and the HTTP transport used for requests.
//
// Construction is lazy. The store and index open on first use, so a missing or
// unusable root disables caching for this client while requests continue
// uncached; construction itself never touches the filesystem.
type Client struct {
	root       string
	config     Config
	httpClient *http.Client
	logger     *log.Logger

	lifetime sync.RWMutex
	closed   bool
	once     sync.Once
	handler  *handler
}

// New constructs a cache client rooted at root. A nil logger records nothing.
// httpClient must be supplied: a nil client makes network helpers fail rather
// than falling back to a process-wide default.
func New(
	root string,
	config Config,
	httpClient *http.Client,
	logger *log.Logger,
) *Client {
	return &Client{
		root:       root,
		config:     config,
		httpClient: httpClient,
		logger:     logger,
	}
}

// Do executes a request through the configured transport so callers that fetch
// uncached bytes share the same HTTP configuration.
func (c *Client) Do(request *http.Request) (*http.Response, error) {
	if c.httpClient == nil {
		return nil, ErrNoHTTPClient
	}
	return c.httpClient.Do(request)
}

// Close waits for cache operations and closes the store if it was opened.
// It is safe to call repeatedly and never initializes an unused store.
func (c *Client) Close() error {
	c.lifetime.Lock()
	defer c.lifetime.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	c.once.Do(func() {
		c.handler = &handler{logger: c.logger}
	})
	c.handler.on = false
	if c.handler.store != nil {
		return c.handler.store.Close()
	}
	return nil
}

func (c *Client) cache() *handler {
	c.once.Do(func() {
		c.handler = newHandler(c.root, c.config, c.logger)
	})
	return c.handler
}

// All returns every live index entry in key order.
func (c *Client) All() []*Entry {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	return c.cache().all()
}

// ClearAll empties the index and reports the objects it collected.
func (c *Client) ClearAll() (ResetReport, error) {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	return c.cache().clearAll()
}

// Get keeps a reader alive independently of later index replacements. The
// caller must Close it.
func (c *Client) Get(k string) (
	hit bool,
	reader io.ReadCloser,
	filename string,
	err error,
) {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	return c.cache().get(k)
}

// GetBytes returns a cached entry's bytes.
func (c *Client) GetBytes(k string) (hit bool, data []byte, err error) {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	return c.cache().getBytes(k)
}

// Exist reports whether k names a live entry.
func (c *Client) Exist(k string) bool {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	return c.cache().exist(k)
}

// Remove drops the entry for k and reclaims unreferenced objects.
func (c *Client) Remove(k string) error {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	return c.cache().remove(k)
}

// Flush publishes the in-memory index under the cache lock.
func (c *Client) Flush() error {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	return c.cache().flush()
}
