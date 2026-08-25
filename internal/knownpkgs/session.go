package knownpkgs

import (
	"sync"

	"github.com/mclucy/lucy/types"
)

// Session is a process-scoped cache layer on top of the persistent store.
// It holds mappings discovered during the current server probe so that
// subsequent resolutions in the same invocation don't re-query providers.
//
// A session is created per command invocation by calling Default().Session().
// The first Session() call for a given process initializes the session; later
// calls return the same instance.
//
// Resolution order in Lookup:
//  1. Session cache (fresh, current probe)
//  2. Persistent store (cross-session, may be stale)
//
// Mappings written via Record are written to both the session and the
// persistent store, so the session is always a strict superset of the
// persisted data.
type Session struct {
	mu     sync.Mutex
	store  *store
	memory map[sessionKey]string
}

type sessionKey struct {
	src     types.SourceId
	localId string
}

// Session returns the process-scoped session bound to the default store.
// Calling it multiple times returns the same session.
func (s *store) Session() *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := sessions[s]; ok {
		return session
	}
	sess := &Session{
		store:  s,
		memory: make(map[sessionKey]string),
	}
	if sessions == nil {
		sessions = make(map[*store]*Session)
	}
	sessions[s] = sess
	return sess
}

var sessions map[*store]*Session

// Lookup resolves a local package name to its canonical ID.
// It checks the session cache first, then the persistent store.
// Returns the canonical ID and true if found.
func (sess *Session) Lookup(src types.SourceId, localId string) (string, bool) {
	k := sessionKey{src: src, localId: localId}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if canon, ok := sess.memory[k]; ok {
		return canon, true
	}
	canon, ok := sess.store.GetLoose(src, localId)
	if ok {
		sess.memory[k] = canon
	}
	return canon, ok
}

// LookupAny resolves a local package name to its canonical ID by trying every
// known source. This is used by the probe resolver when the source is
// unspecified. Returns the canonical ID and the source it was found under.
//
// On a hit, the mapping is promoted to the session cache so subsequent
// lookups skip the store. On a miss, returns ("", SourceUnknown, false).
func (sess *Session) LookupAny(localId string) (string, types.SourceId, bool) {
	sources := []types.SourceId{
		types.SourceModrinth,
		types.SourceCurseForge,
		types.SourceMCDR,
		types.SourceHangar,
		types.SourceSpiget,
		types.SourceGitHub,
	}
	for _, src := range sources {
		if canon, ok := sess.Lookup(src, localId); ok {
			return canon, src, true
		}
	}
	return "", types.SourceUnknown, false
}

// Record stores a mapping in both the session cache and the persistent store.
// Use this when a mapping is discovered (e.g., via hash-based provider query).
func (sess *Session) Record(
	src types.SourceId,
	localId, fileHash, canonicalId, resolvedBy string,
) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.memory[sessionKey{src: src, localId: localId}] = canonicalId
	sess.store.Set(src, localId, fileHash, canonicalId, resolvedBy)
}
