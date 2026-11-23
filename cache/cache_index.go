package cache

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/internal/fsutil"
)

const indexVersion = 3

const (
	// journalSuffix names the append-only record log published beside the
	// snapshot. A process replays it after loading the snapshot.
	journalSuffix = ".journal"
	// journalEntryLimit and journalByteLimit bound the replay a reader pays
	// before the next publish compacts the journal into the snapshot.
	journalEntryLimit = 256
	journalByteLimit  = 1 << 20
)

const (
	journalOpPut    = "put"
	journalOpDelete = "delete"
)

type indexManifest struct {
	Version int            `json:"version"`
	Entries map[key]*Entry `json:"entries"`
}

// journalRecord is one durable index change. A snapshot rewrite replaces the
// whole journal, so records only describe changes made after the snapshot.
type journalRecord struct {
	Op    string `json:"op"`
	Key   key    `json:"key"`
	Entry *Entry `json:"entry,omitempty"`
}

// index holds the cache index in memory for the process. A process takes the
// cache file lock to load changes another process published and to publish its
// own changes; a read only stats the snapshot and journal and reloads when
// their identity changed.
type index struct {
	path    string
	journal string
	entries map[key]*Entry
	// info and journalInfo are the stat identities the in-memory state was
	// loaded from. A write that replaces the snapshot changes its inode, and
	// an append changes the journal size.
	info           os.FileInfo
	journalInfo    os.FileInfo
	journalRecords int
}

func newIndex(path string) *index {
	return &index{
		path:    path,
		journal: path + journalSuffix,
		entries: make(map[key]*Entry),
	}
}

// load brings the in-memory index up to date with the published snapshot and
// journal. It runs under cache ownership when it may repair a broken index.
func (idx *index) load() (bool, error) {
	changed, err := idx.changed()
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	reset, err := idx.loadSnapshot()
	if err != nil {
		return false, err
	}
	if reset {
		idx.entries = make(map[key]*Entry)
		if err := idx.writeSnapshot(); err != nil {
			return false, err
		}
		if err := idx.removeJournal(); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := idx.replayJournal(); err != nil {
		return false, err
	}
	return false, nil
}

// changed reports whether the published state differs from the state this
// process holds. It only stats both files. A missing snapshot counts as a
// change, because a loaded index always has one.
func (idx *index) changed() (bool, error) {
	info, err := os.Stat(idx.path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return true, nil
	}
	if idx.info == nil || !sameFileStamp(info, idx.info) {
		return true, nil
	}
	journalInfo, journalErr := os.Stat(idx.journal)
	if errors.Is(journalErr, os.ErrNotExist) {
		return idx.journalInfo != nil, nil
	}
	if journalErr != nil {
		return true, nil
	}
	if idx.journalInfo == nil || !sameFileStamp(journalInfo, idx.journalInfo) {
		return true, nil
	}
	return false, nil
}

// sameFileStamp matches the identity a file was loaded from against its
// current stat.
func sameFileStamp(current, cached os.FileInfo) bool {
	return cached != nil && current != nil &&
		os.SameFile(current, cached) &&
		current.Size() == cached.Size() &&
		current.ModTime().Equal(cached.ModTime())
}

// loadSnapshot replaces the in-memory entries with the published snapshot. A
// missing, unreadable, or invalid snapshot resets the index and reports it.
func (idx *index) loadSnapshot() (bool, error) {
	file, err := os.Open(idx.path)
	if errors.Is(err, os.ErrNotExist) {
		idx.entries = make(map[key]*Entry)
		idx.info = nil
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var manifest indexManifest
	decodeErr := json.UnmarshalRead(file, &manifest)
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil {
		return false, errors.Join(statErr, closeErr)
	}
	if decodeErr != nil || !validIndex(manifest) {
		idx.entries = make(map[key]*Entry)
		idx.info = nil
		return true, nil
	}
	idx.info = info
	idx.entries = manifest.Entries
	return false, nil
}

func validIndex(manifest indexManifest) bool {
	if manifest.Version != indexVersion || manifest.Entries == nil {
		return false
	}
	for k, entry := range manifest.Entries {
		if !validEntry(k, entry) {
			return false
		}
	}
	return true
}

func validEntry(k key, entry *Entry) bool {
	if entry == nil || entry.Size < 0 || entry.Kind > KindArtifact || entry.Key != string(k) {
		return false
	}
	digest, err := cas.ParseDigest(entry.ContentHash)
	return err == nil &&
		entry.Filename != "" &&
		sanitizeFilename(entry.Filename, digest.Hex()) == entry.Filename
}

// replayJournal applies the records published after the snapshot. A torn or
// invalid tail is discarded, so a crashed writer costs at most the entries it
// had not finished publishing.
func (idx *index) replayJournal() error {
	idx.journalRecords = 0
	data, err := os.ReadFile(idx.journal)
	if errors.Is(err, os.ErrNotExist) {
		idx.journalInfo = nil
		return nil
	}
	if err != nil {
		return err
	}
	offset := 0
	for offset < len(data) {
		end := bytes.IndexByte(data[offset:], '\n')
		if end < 0 {
			break
		}
		var record journalRecord
		line := data[offset : offset+end]
		if json.Unmarshal(line, &record) != nil || !applyJournalRecord(idx.entries, record) {
			break
		}
		offset += end + 1
		idx.journalRecords++
	}
	if offset < len(data) {
		if err := os.Truncate(idx.journal, int64(offset)); err != nil {
			return err
		}
	}
	info, err := os.Stat(idx.journal)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			idx.journalInfo = nil
			return nil
		}
		return err
	}
	idx.journalInfo = info
	return nil
}

func applyJournalRecord(entries map[key]*Entry, record journalRecord) bool {
	switch record.Op {
	case journalOpPut:
		if !validEntry(record.Key, record.Entry) {
			return false
		}
		entries[record.Key] = record.Entry
	case journalOpDelete:
		delete(entries, record.Key)
	default:
		return false
	}
	return true
}

// flush publishes the whole in-memory index as a new snapshot and drops the
// journal that is now redundant.
func (idx *index) flush() error {
	if err := idx.writeSnapshot(); err != nil {
		return err
	}
	return idx.removeJournal()
}

func (idx *index) writeSnapshot() error {
	data, err := json.Marshal(indexManifest{
		Version: indexVersion,
		Entries: idx.entries,
	})
	if err != nil {
		idx.info = nil
		return fmt.Errorf("encode cache index: %w", err)
	}
	if err := fsutil.AtomicWrite(idx.path, data, 0o600); err != nil {
		idx.info = nil
		return fmt.Errorf("publish cache index: %w", err)
	}
	idx.info, err = os.Stat(idx.path)
	return err
}

func (idx *index) removeJournal() error {
	idx.journalRecords = 0
	idx.journalInfo = nil
	if err := os.Remove(idx.journal); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear cache journal: %w", err)
	}
	return nil
}

// publishRecords appends records for changes already applied in memory. When
// the journal outgrows its replay bound, the same call compacts it.
func (idx *index) publishRecords(records []journalRecord) error {
	idx.journalRecords += len(records)
	if err := idx.appendJournal(records); err != nil {
		return err
	}
	if idx.journalRecords >= journalEntryLimit ||
		idx.journalInfo != nil && idx.journalInfo.Size() >= journalByteLimit {
		return idx.flush()
	}
	return nil
}

// appendJournal writes one batch of records. The caller holds the cache lock,
// so records from different processes never interleave.
func (idx *index) appendJournal(records []journalRecord) error {
	if len(records) == 0 {
		return nil
	}
	var buffer bytes.Buffer
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("encode cache journal record: %w", err)
		}
		buffer.Write(line)
		buffer.WriteByte('\n')
	}
	file, err := os.OpenFile(
		idx.journal,
		os.O_WRONLY|os.O_CREATE|os.O_APPEND,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("open cache journal: %w", err)
	}
	_, writeErr := file.Write(buffer.Bytes())
	info, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(writeErr, statErr, closeErr); err != nil {
		return fmt.Errorf("append cache journal: %w", err)
	}
	idx.journalInfo = info
	return nil
}

func (idx *index) get(k key) (*Entry, bool) {
	entry, ok := idx.entries[k]
	return entry, ok
}

func (idx *index) put(k key, entry *Entry) {
	idx.entries[k] = entry
}

func (idx *index) delete(k key) {
	delete(idx.entries, k)
}
