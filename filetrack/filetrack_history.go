package filetrack

import (
	"bufio"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/manifest"
)

const (
	historyName     = ".lucy/history/files.jsonl"
	transactionName = ".lucy/file-commit.json"
)

// History is read-only and includes events for paths that are no longer tracked.
func History(root, path string) ([]Event, error) {
	if err := noPending(root); err != nil {
		return nil, err
	}
	if path != "" {
		var err error
		path, err = input.TrackedPath(path)
		if err != nil {
			return nil, err
		}
	}
	events, _, err := readHistory(root, false)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return events, nil
	}
	selected := make([]Event, 0)
	for _, event := range events {
		changes := make([]Change, 0)
		for _, change := range event.Changes {
			if change.Path == path {
				changes = append(changes, change)
			}
		}
		if len(changes) != 0 {
			event.Changes = changes
			selected = append(selected, event)
		}
	}
	return selected, nil
}

func noPending(root string) error {
	for _, name := range []string{transactionName, workspacewrite.InstallJournalName} {
		if err := workspacewrite.SafePath(root, name); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return fmt.Errorf("unfinished workspace transaction; run a mutating Lucy command to recover it")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func readHistory(root string, repair bool) (events []Event, offset int64, resultErr error) {
	if err := workspacewrite.SafePath(root, historyName); err != nil {
		return nil, 0, err
	}
	flag := os.O_RDONLY
	if repair {
		flag = os.O_RDWR
	}
	file, err := os.OpenFile(filepath.Join(root, historyName), flag, 0)
	if errors.Is(err, os.ErrNotExist) {
		return []Event{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	reader := bufio.NewReader(file)
	events = make([]Event, 0)
	identities := map[string]bool{}
	for {
		line, err := reader.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			if len(line) != 0 {
				if !repair {
					return nil, offset, fmt.Errorf("incomplete file history tail; run a mutating file command to recover it")
				}
				if err := file.Truncate(offset); err != nil {
					return nil, offset, err
				}
				if err := file.Sync(); err != nil {
					return nil, offset, err
				}
			}
			return events, offset, nil
		}
		if err != nil {
			return nil, offset, err
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, offset, fmt.Errorf("corrupt file history at byte %d: %w", offset, err)
		}
		if identities[event.ID] {
			return nil, offset, fmt.Errorf("duplicate file history identity at byte %d", offset)
		}
		identities[event.ID] = true
		if err := validateEvent(events, event); err != nil {
			return nil, offset, fmt.Errorf("invalid file history at byte %d: %w", offset, err)
		}
		events = append(events, event)
		offset += int64(len(line))
	}
}

func validateEvent(previous []Event, event Event) error {
	_, zone := event.Time.Zone()
	if event.Sequence != uint64(len(previous)+1) || event.ID == "" || event.Time.IsZero() || zone != 0 || len(event.Changes) == 0 {
		return fmt.Errorf("invalid event identity")
	}
	switch event.Operation {
	case Track, Untrack, Update, Recover, Revert, Lock, Unlock:
	default:
		return fmt.Errorf("unknown operation")
	}
	paths := make(map[string]bool, len(event.Changes))
	for _, change := range event.Changes {
		path, err := input.TrackedPath(change.Path)
		if err != nil || path != change.Path || paths[path] || change.Parent >= event.Sequence || change.Revision > event.Sequence || (change.Mode|change.BackupMode)&^uint32(0o777) != 0 {
			return fmt.Errorf("invalid change for %s", change.Path)
		}
		paths[path] = true
		for _, declaration := range []*manifest.TrackedFile{change.Before, change.After} {
			if declaration == nil {
				continue
			}
			copy := []manifest.TrackedFile{*declaration}
			if err := manifest.NormalizeTrackedFiles(copy); err != nil || copy[0] != *declaration || declaration.Path != path {
				return fmt.Errorf("invalid declaration for %s", path)
			}
		}
		if change.BackupHash != "" {
			if _, err := cas.ParseDigest(change.BackupHash); err != nil {
				return fmt.Errorf("invalid working backup for %s", path)
			}
		}
		if change.Accepted {
			if (event.Operation != Track && event.Operation != Update && event.Operation != Lock) || change.After == nil || change.After.Hash == "" || change.Revision != event.Sequence {
				return fmt.Errorf("invalid accepted revision for %s", path)
			}
			if change.Parent != 0 {
				if _, ok := revisionAt(previous, path, change.Parent); !ok {
					return fmt.Errorf("missing parent revision for %s", path)
				}
			}
		} else if change.Revision != 0 {
			revision, ok := revisionAt(previous, path, change.Revision)
			if !ok || (change.After != nil && change.After.Hash != revision.hash) {
				return fmt.Errorf("missing active revision for %s", path)
			}
		}
	}
	return nil
}

type acceptedRevision struct {
	sequence uint64
	parent   uint64
	hash     string
	mode     os.FileMode
}

func revisionAt(events []Event, path string, sequence uint64) (acceptedRevision, bool) {
	if sequence == 0 || sequence > uint64(len(events)) {
		return acceptedRevision{}, false
	}
	for _, change := range events[sequence-1].Changes {
		if change.Path == path && change.Accepted && change.After != nil {
			return acceptedRevision{sequence: sequence, parent: change.Parent, hash: change.After.Hash, mode: os.FileMode(change.Mode)}, true
		}
	}
	return acceptedRevision{}, false
}

func currentRevision(events []Event, path, hash string) acceptedRevision {
	for i := len(events) - 1; i >= 0; i-- {
		for _, change := range events[i].Changes {
			if change.Path == path && change.Revision != 0 {
				revision, ok := revisionAt(events, path, change.Revision)
				if ok && revision.hash == hash {
					return revision
				}
				return acceptedRevision{}
			}
		}
	}
	return acceptedRevision{}
}

func revertRevision(events []Event, path, acceptedHash, hash string, sequence uint64) (acceptedRevision, error) {
	if hash != "" && sequence != 0 {
		return acceptedRevision{}, fmt.Errorf("hash and --revision are mutually exclusive")
	}
	if sequence != 0 {
		if revision, ok := revisionAt(events, path, sequence); ok {
			return revision, nil
		}
		return acceptedRevision{}, fmt.Errorf("accepted revision %d is not available for %s", sequence, path)
	}
	if hash != "" {
		for i := len(events) - 1; i >= 0; i-- {
			if revision, ok := revisionAt(events, path, uint64(i+1)); ok && revision.hash == hash {
				return revision, nil
			}
		}
		return acceptedRevision{}, fmt.Errorf("accepted revision %s is not available for %s", hash, path)
	}
	current := currentRevision(events, path, acceptedHash)
	if previous, ok := revisionAt(events, path, current.parent); ok {
		return previous, nil
	}
	return acceptedRevision{}, fmt.Errorf("no previous accepted revision for %s", path)
}
