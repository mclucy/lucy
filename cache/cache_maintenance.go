package cache

import (
	"time"

	"github.com/mclucy/lucy/cas"
)

const manifestFilename = "cache.json"

type key string

// ResetReport counts unique objects actually removed, collapsing shared URL references.
// Removed lists the paths of those objects.
type ResetReport struct {
	TotalFreedSize int64
	FileCount      int
	Removed        []string
}

func (h *handler) maintainLocked(removed []cas.Digest) ([]cas.Digest, []key, error) {
	var dropped []key
	for _, k := range expiredEntries(h.index.entries, time.Now()) {
		digest, err := cas.ParseDigest(h.index.entries[k].ContentHash)
		if err != nil {
			return nil, nil, err
		}
		removed = append(removed, digest)
		dropped = append(dropped, k)
		h.index.delete(k)
	}
	for _, target := range evictionCandidates(h.index.entries, h.policy) {
		digest, err := cas.ParseDigest(h.index.entries[target.key].ContentHash)
		if err != nil {
			return nil, nil, err
		}
		removed = append(removed, digest)
		dropped = append(dropped, target.key)
		h.index.delete(target.key)
	}
	return removed, dropped, nil
}

func (h *handler) referencedLocked(digest cas.Digest) bool {
	value := digest.String()
	for _, entry := range h.index.entries {
		if entry.ContentHash == value {
			return true
		}
	}
	return false
}

func (h *handler) collectCandidatesLocked(digests []cas.Digest) error {
	for _, digest := range digests {
		if h.referencedLocked(digest) {
			continue
		}
		if _, err := h.store.Remove(digest); err != nil {
			return err
		}
	}
	return nil
}

func (h *handler) collectAllLocked(report *ResetReport) error {
	references := make(map[cas.Digest]struct{}, len(h.index.entries))
	for _, entry := range h.index.entries {
		digest, err := cas.ParseDigest(entry.ContentHash)
		if err != nil {
			return err
		}
		references[digest] = struct{}{}
	}
	return h.store.Walk(func(object cas.Object) error {
		if _, ok := references[object.Digest]; ok {
			return nil
		}
		freed, err := h.store.Remove(object.Digest)
		if err != nil {
			return err
		}
		if report != nil {
			report.TotalFreedSize += freed
			report.FileCount++
			report.Removed = append(report.Removed, h.store.Path(object.Digest))
		}
		return nil
	})
}
