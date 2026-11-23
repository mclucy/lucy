package toolchain

import (
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/mclucy/lucy/internal/fsutil"
)

// probeCacheVersion is the layout version of the probe cache file. A file of
// another version is ignored.
const probeCacheVersion = 1

// probeCacheFilename names the probe cache inside the toolchain directory of
// Lucy's per-user cache.
const probeCacheFilename = "java-probes.json"

// probeCacheFile is the persisted form of the probe cache.
type probeCacheFile struct {
	Version int                   `json:"version"`
	Entries map[string]probeEntry `json:"entries"`
}

// probeEntry records one Java probe result. The map key is the resolved Java
// executable. JavaHome is the JAVA_HOME of the probing process: changing it
// makes a stub executable start another runtime, so it keys the entry too.
type probeEntry struct {
	JavaHome     string           `json:"java_home"`
	Executable   fileIdentity     `json:"executable"`
	Release      *fileIdentity    `json:"release,omitempty"`
	Installation JavaInstallation `json:"installation"`
}

// probeCache is the in-memory view of the probe cache file. Its methods are
// safe for concurrent use, and a nil cache stores nothing. Every cache
// failure costs a probe, never a result.
type probeCache struct {
	mutex   sync.Mutex
	path    string
	entries map[string]probeEntry
	dirty   bool
}

// loadProbeCache reads the cache of this user. A missing, unreadable, or
// corrupt file yields an empty cache that the next save rewrites.
func loadProbeCache() *probeCache {
	cache := &probeCache{
		path:    probeCachePath(),
		entries: make(map[string]probeEntry),
	}
	if cache.path == "" {
		return cache
	}
	data, err := os.ReadFile(cache.path)
	if err != nil {
		cache.dirty = !errors.Is(err, os.ErrNotExist)
		return cache
	}
	var file probeCacheFile
	if err := json.Unmarshal(data, &file); err != nil || file.Version != probeCacheVersion {
		cache.dirty = true
		return cache
	}
	for key, entry := range file.Entries {
		cache.entries[key] = entry
	}
	return cache
}

// lookup returns the recorded installation of executable while the executable,
// the release file of its home, and JAVA_HOME keep the recorded values.
func (c *probeCache) lookup(executable string) (JavaInstallation, bool) {
	if c == nil {
		return JavaInstallation{}, false
	}
	key := canonical(executable)
	c.mutex.Lock()
	entry, ok := c.entries[key]
	c.mutex.Unlock()
	if !ok || entry.JavaHome != os.Getenv("JAVA_HOME") {
		return JavaInstallation{}, false
	}
	identity, ok := identify(key)
	if !ok || identity != entry.Executable {
		return JavaInstallation{}, false
	}
	if !releaseMatches(entry.Installation.Home, entry.Release) {
		return JavaInstallation{}, false
	}
	return entry.Installation, true
}

// store records a probe result without writing the file. The installation must
// carry no compiler: a compiler belongs to the build discovery that probed it.
func (c *probeCache) store(executable string, installation JavaInstallation) {
	if c == nil {
		return
	}
	key := canonical(executable)
	identity, ok := identify(key)
	if !ok {
		return
	}
	entry := probeEntry{
		JavaHome:     os.Getenv("JAVA_HOME"),
		Executable:   identity,
		Installation: installation,
	}
	if release, present := identify(filepath.Join(installation.Home, "release")); present {
		entry.Release = &release
	}
	c.mutex.Lock()
	c.entries[key] = entry
	c.dirty = true
	c.mutex.Unlock()
}

// save writes the cache atomically and drops entries whose executables are
// gone. It reports nothing: an unwritable cache only costs later probes.
func (c *probeCache) save() {
	if c == nil {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if !c.dirty || c.path == "" {
		return
	}
	entries := make(map[string]probeEntry, len(c.entries))
	for key, entry := range c.entries {
		if _, err := os.Stat(key); err != nil {
			continue
		}
		entries[key] = entry
	}
	data, err := json.Marshal(probeCacheFile{Version: probeCacheVersion, Entries: entries})
	if err != nil {
		return
	}
	if err := fsutil.AtomicWrite(c.path, data, 0o600); err != nil {
		return
	}
	c.entries = entries
	c.dirty = false
}

// releaseMatches reports whether the release file of home still has the
// recorded identity. A home without a release file matches only a recorded
// absence.
func releaseMatches(home string, recorded *fileIdentity) bool {
	identity, present := identify(filepath.Join(home, "release"))
	return present == (recorded != nil) && (!present || identity == *recorded)
}

// probeCachePath reports the probe cache file beneath Lucy's per-user cache
// directory, in the layout of the network cache. It reports "" when the
// platform names no cache directory.
func probeCachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "lucy", "toolchain", probeCacheFilename)
}

// fileIdentity is the stat identity of a file. A rewrite, replacement, or
// touch changes at least one field, so equal identities describe the same
// generation of the file. Platforms without device or inode numbers leave
// those fields zero and rely on the remaining fields.
type fileIdentity struct {
	Device   uint64 `json:"device"`
	Inode    uint64 `json:"inode"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified_ns"`
	Changed  int64  `json:"changed_ns"`
}

// identify reports the stat identity of path, and false when path cannot be
// inspected.
func identify(path string) (fileIdentity, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return fileIdentity{}, false
	}
	return identityOf(info), true
}
