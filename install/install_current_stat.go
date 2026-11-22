package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mclucy/lucy/internal/fileidentity"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
	"go.yaml.in/yaml/v4"
)

// currentStatFilename records the stat identity of every receipt file whose
// accepted digest the last command verified. It is evidence, not ownership:
// a missing or invalid record only sends the next check back to the content.
const currentStatFilename = ".lucy/current.yaml"

const currentStatVersion = 1

type currentStatDocument struct {
	Version int                 `yaml:"version"`
	Files   []currentStatRecord `yaml:"files"`
}

type currentStatRecord struct {
	Path     string                `yaml:"path"`
	SHA256   string                `yaml:"sha256"`
	Identity fileidentity.Identity `yaml:"identity"`
}

// currentEvidence answers a currentness check from stat identities a previous
// command recorded, and collects the identities this command proved so an
// unchanged file is read once across commands.
type currentEvidence struct {
	memo       *fileidentity.Memo
	records    map[string]currentStatRecord
	recordedNS int64
	raw        []byte
	observed   map[string]currentStatRecord
	mu         sync.Mutex
}

// newCurrentEvidence reads the stat sidecar. Any failure falls back to
// hashing, because the sidecar only saves work and never authorizes a change.
func newCurrentEvidence(
	root string,
	memo *fileidentity.Memo,
	logger *log.Logger,
) *currentEvidence {
	evidence := &currentEvidence{
		memo:     memo,
		records:  map[string]currentStatRecord{},
		observed: map[string]currentStatRecord{},
	}
	if err := workspacewrite.SafePath(root, currentStatFilename); err != nil {
		logger.Debug("current stat record unavailable", "reason", err.Error())
		return evidence
	}
	target := filepath.Join(root, currentStatFilename)
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return evidence
	}
	if err != nil || !info.Mode().IsRegular() {
		logger.Debug("current stat record unavailable", "reason", "not a regular file")
		return evidence
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		logger.Debug("current stat record unreadable", "reason", err.Error())
		return evidence
	}
	var document currentStatDocument
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&document); err != nil ||
		document.Version != currentStatVersion {
		logger.Debug("current stat record invalid", "reason", "schema mismatch")
		return evidence
	}
	for _, record := range document.Files {
		evidence.records[record.Path] = record
	}
	evidence.recordedNS = info.ModTime().UnixNano()
	evidence.raw = raw
	return evidence
}

// trusted reports whether the recorded identity still describes entry's file.
// A record only counts when its digest matches the receipt and the file's
// change time predates the record, so a coarse timestamp cannot hide a write
// in the same granule.
func (e *currentEvidence) trusted(entry receipt.Entry, info fs.FileInfo) bool {
	if e == nil {
		return false
	}
	record, ok := e.records[entry.Path]
	if !ok || !strings.EqualFold(record.SHA256, entry.SHA256) {
		return false
	}
	identity, ok := fileidentity.Of(info)
	if !ok || !identity.Same(record.Identity) {
		return false
	}
	return identity.OlderThan(e.recordedNS)
}

func (e *currentEvidence) observe(
	entry receipt.Entry,
	identity fileidentity.Identity,
	hasIdentity bool,
) {
	if e == nil || !hasIdentity {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.observed[entry.Path] = currentStatRecord{
		Path:     entry.Path,
		SHA256:   strings.ToLower(entry.SHA256),
		Identity: identity,
	}
}

// write publishes the identities this command proved for entries. It keeps
// records an earlier command proved and the receipt still names, and leaves an
// unchanged record byte-identical. It never fails the command.
func (e *currentEvidence) write(
	root string,
	entries []receipt.Entry,
	logger *log.Logger,
) {
	if e == nil {
		return
	}
	e.mu.Lock()
	observed := make(map[string]currentStatRecord, len(e.observed))
	for path, record := range e.observed {
		observed[path] = record
	}
	carried := make(map[string]currentStatRecord, len(e.records))
	for path, record := range e.records {
		carried[path] = record
	}
	e.mu.Unlock()
	records := make([]currentStatRecord, 0, len(entries))
	for _, entry := range entries {
		record, ok := observed[entry.Path]
		if !ok {
			record, ok = carried[entry.Path]
		}
		if !ok || !strings.EqualFold(record.SHA256, entry.SHA256) {
			continue
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return
	}
	slices.SortFunc(records, func(left, right currentStatRecord) int {
		return strings.Compare(left.Path, right.Path)
	})
	raw, err := yaml.Marshal(currentStatDocument{
		Version: currentStatVersion,
		Files:   records,
	})
	if err != nil {
		logger.Warn(fmt.Errorf("encode current stat record: %w", err))
		return
	}
	if bytes.Equal(raw, e.raw) {
		return
	}
	target := filepath.Join(root, currentStatFilename)
	if err := fsutil.AtomicWrite(target, raw, 0o600); err != nil {
		logger.Warn(fmt.Errorf("write current stat record: %w", err))
	}
}
