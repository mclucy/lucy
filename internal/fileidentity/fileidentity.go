// Package fileidentity identifies one version of a file by the filesystem
// metadata that every write changes, and memoizes content digests per command
// so a file that nothing touched is read once.
package fileidentity

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// memoRacyWindow keeps a digest out of the memo while the file's change time
// could share a timestamp granule with a later edit. Two seconds covers the
// coarsest filesystems Lucy targets.
const memoRacyWindow = 2 * time.Second

// Identity names one version of a file. Device and inode catch a replacement,
// size catches an append, and the timestamps catch an in-place write or mode
// change. YAML tags let a sidecar persist the evidence.
type Identity struct {
	Device  uint64 `yaml:"device"`
	Inode   uint64 `yaml:"inode"`
	Size    int64  `yaml:"size"`
	MtimeNS int64  `yaml:"mtime_ns"`
	CtimeNS int64  `yaml:"ctime_ns"`
}

// Of extracts a file's stat identity. It reports false on a platform or
// filesystem that exposes none, and callers then read the content every time.
func Of(info fs.FileInfo) (Identity, bool) {
	return identityFromInfo(info)
}

// Same reports whether two identities describe the same file version.
func (i Identity) Same(other Identity) bool {
	return i == other
}

// Racy reports whether the change time is too recent for the identity to
// exclude a write in the same timestamp granule.
func (i Identity) Racy(now time.Time) bool {
	return !time.Unix(0, i.CtimeNS).Before(now.Add(-memoRacyWindow))
}

// OlderThan reports whether the file changed strictly before the recorded
// filesystem timestamp. A coarse timestamp makes the two equal, which refuses
// the evidence instead of risking a same-granule edit.
func (i Identity) OlderThan(timestampNS int64) bool {
	return i.CtimeNS < timestampNS
}

// Observation is one observation of a path. Exists is false for a missing
// path; Regular is false for a directory or symbolic link. Identity is
// recorded even while a file is too recent for the memo, so a caller can
// persist the evidence once the write is old enough.
type Observation struct {
	Exists      bool
	Regular     bool
	Mode        fs.FileMode
	Size        int64
	Hashes      map[string]string
	Identity    Identity
	HasIdentity bool
}

// Memo caches content digests by stat identity for one command.
type Memo struct {
	mu    sync.Mutex
	files map[string]memoFile
}

type memoFile struct {
	identity Identity
	hashes   map[string]string
}

// NewMemo returns an empty digest memo.
func NewMemo() *Memo {
	return &Memo{files: make(map[string]memoFile)}
}

// ObserveRoot observes rel through files. It reads the content only when the
// memo holds no identity that matches the path now.
func (m *Memo) ObserveRoot(files *os.Root, rel string, algorithms []string) (Observation, error) {
	rel = filepath.FromSlash(rel)
	info, err := files.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return Observation{}, nil
	}
	if err != nil {
		return Observation{}, err
	}
	key := files.Name() + "\x00" + rel
	return m.observe(key, rel, info, files.Open, algorithms)
}

// ObserveRootInfo observes rel through files using an info the caller already
// observed for the same path.
func (m *Memo) ObserveRootInfo(
	files *os.Root,
	rel string,
	info fs.FileInfo,
	algorithms []string,
) (Observation, error) {
	rel = filepath.FromSlash(rel)
	key := files.Name() + "\x00" + rel
	return m.observe(key, rel, info, files.Open, algorithms)
}

// ObservePath observes an absolute path.
func (m *Memo) ObservePath(path string, algorithms []string) (Observation, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Observation{}, nil
	}
	if err != nil {
		return Observation{}, err
	}
	return m.observe(path, path, info, os.Open, algorithms)
}

func (m *Memo) observe(
	key string,
	name string,
	info fs.FileInfo,
	open func(string) (*os.File, error),
	algorithms []string,
) (Observation, error) {
	observation := Observation{
		Exists:      true,
		Regular:     info.Mode().IsRegular(),
		Mode:        info.Mode(),
		Size:        info.Size(),
		HasIdentity: false,
	}
	if identity, ok := identityFromInfo(info); ok {
		observation.Identity = identity
		observation.HasIdentity = true
		if hashes, hit := m.lookup(key, identity, algorithms); hit {
			observation.Hashes = hashes
			return observation, nil
		}
	}
	if !observation.Regular {
		return observation, nil
	}
	file, err := open(name)
	if err != nil {
		return Observation{}, err
	}
	hashes, identity, identified, readErr := digestFile(file, algorithms)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return Observation{}, errors.Join(readErr, closeErr)
	}
	observation.Hashes = hashes
	if identified {
		observation.Identity = identity
		observation.HasIdentity = true
		if !identity.Racy(time.Now()) {
			m.store(key, identity, hashes)
		}
	}
	return observation, nil
}

func (m *Memo) lookup(key string, identity Identity, algorithms []string) (map[string]string, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	entry, ok := m.files[key]
	m.mu.Unlock()
	if !ok || !entry.identity.Same(identity) {
		return nil, false
	}
	hashes := make(map[string]string, len(algorithms)+1)
	hashes["sha256"] = entry.hashes["sha256"]
	if hashes["sha256"] == "" {
		return nil, false
	}
	for _, algorithm := range algorithms {
		if algorithm == "sha256" {
			continue
		}
		digest := entry.hashes[algorithm]
		if digest == "" {
			return nil, false
		}
		hashes[algorithm] = digest
	}
	return hashes, true
}

func (m *Memo) store(key string, identity Identity, hashes map[string]string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[key] = memoFile{
		identity: identity,
		hashes:   cloneHashes(hashes),
	}
}

func cloneHashes(hashes map[string]string) map[string]string {
	clone := make(map[string]string, len(hashes))
	for algorithm, digest := range hashes {
		clone[algorithm] = digest
	}
	return clone
}

// digestFile reads file once and returns the SHA-256 and every requested
// algorithm. The identity is read from the same handle after the bytes, so the
// digest and the identity describe the same content.
func digestFile(
	file *os.File,
	algorithms []string,
) (map[string]string, Identity, bool, error) {
	hashers := map[string]hash.Hash{"sha256": sha256.New()}
	writers := []io.Writer{hashers["sha256"]}
	for _, algorithm := range algorithms {
		if _, known := hashers[algorithm]; known {
			continue
		}
		var hasher hash.Hash
		switch algorithm {
		case "sha1":
			hasher = sha1.New()
		case "sha512":
			hasher = sha512.New()
		case "md5":
			hasher = md5.New()
		default:
			return nil, Identity{}, false, fmt.Errorf(
				"unsupported artifact digest %s", algorithm,
			)
		}
		hashers[algorithm] = hasher
		writers = append(writers, hasher)
	}
	if _, err := io.Copy(io.MultiWriter(writers...), file); err != nil {
		return nil, Identity{}, false, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, Identity{}, false, err
	}
	hashes := make(map[string]string, len(hashers))
	for algorithm, hasher := range hashers {
		hashes[algorithm] = hex.EncodeToString(hasher.Sum(nil))
	}
	identity, ok := identityFromInfo(info)
	return hashes, identity, ok, nil
}
