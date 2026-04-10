package slugmap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/mclucy/lucy/global"
	"github.com/mclucy/lucy/types"
)

// Entry is one resolved mapping.
type Entry struct {
	Source        types.Source `json:"source"`
	LocalId       string       `json:"local_id"`
	FileHash      string       `json:"file_hash"`
	CanonicalSlug string       `json:"canonical_slug"`
	// ResolvedBy is always "hash" — only hash-verified slugs are persisted.
	ResolvedBy string `json:"resolved_by"`
}

type store struct {
	mu      sync.RWMutex
	path    string
	entries map[string]Entry // key: source+"/"+localId or source+"/"+localId+"/"+fileHash
}

var defaultStore *store
var once sync.Once

func Default() *store {
	once.Do(func() {
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		}
		p := filepath.Join(dir, global.ProgramName, "slugmap.json")
		defaultStore = &store{path: p, entries: make(map[string]Entry)}
		_ = defaultStore.load()
	})
	return defaultStore
}

func preciseKey(src types.Source, localId, fileHash string) string {
	return string(src) + "/" + localId + "/" + fileHash
}

func looseKey(src types.Source, localId string) string {
	return string(src) + "/" + localId
}

func (s *store) Set(src types.Source, localId, fileHash, canonicalSlug, resolvedBy string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[preciseKey(src, localId, fileHash)] = Entry{
		Source:        src,
		LocalId:       localId,
		FileHash:      fileHash,
		CanonicalSlug: canonicalSlug,
		ResolvedBy:    resolvedBy,
	}
	s.entries[looseKey(src, localId)] = Entry{
		Source:        src,
		LocalId:       localId,
		FileHash:      "",
		CanonicalSlug: canonicalSlug,
		ResolvedBy:    resolvedBy,
	}
	_ = s.flush()
}

func (s *store) Get(src types.Source, localId, fileHash string) (slug string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[preciseKey(src, localId, fileHash)]
	if !ok {
		return "", false
	}
	return e.CanonicalSlug, true
}

func (s *store) GetLoose(src types.Source, localId string) (slug string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[looseKey(src, localId)]
	if !ok {
		return "", false
	}
	return e.CanonicalSlug, true
}

func (s *store) All() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, 0, len(s.entries)/2)
	for _, e := range s.entries {
		if e.FileHash != "" {
			out = append(out, e)
		}
	}
	return out
}

func (s *store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]Entry)
	_ = s.flush()
}

func (s *store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.entries)
}

func (s *store) flush() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}
