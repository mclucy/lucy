package filetrack

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	serverpkg "github.com/mclucy/lucy/server"
	"gopkg.in/yaml.v3"
)

// Run prepares every selected file before making any accepted-state change.
func Run(ctx context.Context, root string, request Request) (result Result, resultErr error) {
	if err := validateRequest(request); err != nil {
		return result, err
	}
	result.Event.Operation = request.Operation
	root, err := filepath.Abs(root)
	if err != nil {
		return result, err
	}
	lock, err := workspacewrite.Lock(root)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return result, err
	}
	if err := RecoverPending(root); err != nil {
		return result, err
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	store, err := cas.New(filepath.Join(root, ".lucy"))
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	if err := store.Cleanup(); err != nil {
		return result, err
	}
	events, historySize, err := readHistory(root, true)
	if err != nil {
		return result, err
	}
	beforeManifest, err := capture(root, files, store, manifest.Filename)
	if err != nil {
		return result, err
	}
	if !beforeManifest.Exists {
		return result, fmt.Errorf("no %s in this workspace; run lucy init first", manifest.Filename)
	}
	beforeLock, err := capture(root, files, store, lockfile.Filename)
	if err != nil {
		return result, err
	}
	reader, err := imageReader(store, beforeManifest)
	if err != nil {
		return result, err
	}
	document, decodeErr := manifest.Decode(reader)
	closeErr := reader.Close()
	if decodeErr != nil {
		return result, errors.Join(decodeErr, closeErr)
	}
	if err := closeErr; err != nil {
		return result, err
	}
	own := make(map[string]struct{})
	record := func(state image) {
		if state.Exists && state.Hash != "" {
			own[state.Hash] = struct{}{}
		}
	}
	record(beforeManifest)
	record(beforeLock)
	defer func() {
		reclaimed, sweepErr := reclaimObjects(root, files, store, own)
		if sweepErr != nil {
			log.Warn(sweepErr)
		}
		result.Reclaimed = reclaimed
		if reclaimed.Objects > 0 {
			log.Info(
				"reclaimed unreferenced objects",
				"objects", reclaimed.Objects,
				"bytes", reclaimed.Bytes,
			)
		}
	}()
	if err := workspacewrite.CheckInactive(root, "changing tracked files"); err != nil {
		return result, err
	}
	var locked *lockfile.Document
	currentLock := false
	if beforeLock.Exists {
		reader, err := imageReader(store, beforeLock)
		if err != nil {
			return result, err
		}
		locked, decodeErr = lockfile.Decode(reader)
		closeErr := reader.Close()
		if decodeErr != nil {
			return result, errors.Join(decodeErr, closeErr)
		}
		if err := closeErr; err != nil {
			return result, err
		}
		fingerprint, err := manifest.Fingerprint(document)
		if err != nil {
			return result, err
		}
		currentLock = locked.ManifestFingerprint == fingerprint
	}
	selected, skipped, err := selectFiles(document.Files, request)
	if err != nil {
		return result, err
	}
	result.Skipped = skipped
	if len(selected) == 0 {
		return result, nil
	}
	event := Event{Sequence: uint64(len(events) + 1), Time: time.Now().UTC(), ID: rand.Text(), Operation: request.Operation, Changes: make([]Change, 0, len(selected))}
	txn := transaction{HistorySize: historySize}
	nextFiles := slices.Clone(document.Files)
	for _, path := range selected {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		before, err := capture(root, files, store, path)
		if err != nil {
			return result, err
		}
		index := slices.IndexFunc(nextFiles, func(file manifest.TrackedFile) bool { return file.Path == path })
		var previous *manifest.TrackedFile
		if index >= 0 {
			copy := nextFiles[index]
			previous = &copy
		}
		change, after, err := prepare(ctx, store, events, event.Sequence, request, path, previous, before)
		if err != nil {
			return result, err
		}
		event.Changes = append(event.Changes, change)
		txn.Mutations = append(txn.Mutations, mutation{Path: path, Before: before, After: after})
		if change.After == nil {
			nextFiles = slices.Delete(nextFiles, index, index+1)
		} else if index >= 0 {
			nextFiles[index] = *change.After
		} else {
			nextFiles = append(nextFiles, *change.After)
		}
	}
	document.Files = nextFiles
	if err := manifest.Normalize(document); err != nil {
		return result, err
	}
	if err := ValidateOwnership(root, document.Files, locked); err != nil {
		return result, err
	}
	raw, err := manifest.CanonicalBytes(document)
	if err != nil {
		return result, err
	}
	afterManifest, err := storeBytes(store, raw, os.FileMode(beforeManifest.Mode))
	if err != nil {
		return result, err
	}
	record(afterManifest)
	txn.Mutations = append(txn.Mutations, mutation{Path: manifest.Filename, Before: beforeManifest, After: afterManifest})
	if locked != nil {
		locked.Files = slices.Clone(document.Files)
		if currentLock {
			locked.ManifestFingerprint, err = manifest.Fingerprint(document)
			if err != nil {
				return result, err
			}
		}
		if err := locked.Validate(); err != nil {
			return result, err
		}
		raw, err := lockfile.Snapshot(locked)
		if err != nil {
			return result, err
		}
		afterLock, err := storeBytes(store, raw, os.FileMode(beforeLock.Mode))
		if err != nil {
			return result, err
		}
		record(afterLock)
		txn.Mutations = append(txn.Mutations, mutation{Path: lockfile.Filename, Before: beforeLock, After: afterLock})
	}
	txn.Event = event
	if err := commit(root, files, store, txn); err != nil {
		return result, err
	}
	result.Event = event
	return result, nil
}

func validateRequest(request Request) error {
	switch request.Operation {
	case Track, Untrack, Update, Recover, Revert, Lock, Unlock:
	default:
		return fmt.Errorf("unknown file operation %q", request.Operation)
	}
	if request.All {
		if request.Path != "" || (request.Operation != Update && request.Operation != Recover) {
			return fmt.Errorf("--all requires update or recover without a path")
		}
	} else if request.Path == "" {
		return fmt.Errorf("file operation requires a path")
	}
	if request.FromSource && request.Operation != Update {
		return fmt.Errorf("--from-source requires update")
	}
	if (request.Immutable || request.NoHash || request.RecoverURL != "") && request.Operation != Track {
		return fmt.Errorf("tracking choices require track")
	}
	if request.NoHash && (request.Immutable || request.RecoverURL != "") {
		return fmt.Errorf("--no-hash conflicts with --immutable and --recover-url")
	}
	if (request.Hash != "" || request.Revision != 0) && request.Operation != Revert {
		return fmt.Errorf("revision targets require revert")
	}
	if request.Hash != "" && request.Revision != 0 {
		return fmt.Errorf("hash and --revision are mutually exclusive")
	}
	return nil
}

func selectFiles(files []manifest.TrackedFile, request Request) ([]string, []string, error) {
	if !request.All {
		path, err := input.TrackedPath(request.Path)
		if err != nil {
			return nil, nil, err
		}
		index := slices.IndexFunc(files, func(file manifest.TrackedFile) bool { return file.Path == path })
		if request.Operation == Track && index >= 0 {
			return nil, nil, fmt.Errorf("file %s is already tracked; use update", path)
		}
		if request.Operation != Track && index < 0 {
			return nil, nil, fmt.Errorf("file %s is not tracked", path)
		}
		return []string{path}, nil, nil
	}
	selected, skipped := make([]string, 0, len(files)), make([]string, 0)
	for _, file := range files {
		if (request.FromSource && file.RecoverURL == "") || (request.Operation == Recover && file.Hash == "") {
			skipped = append(skipped, file.Path)
			continue
		}
		selected = append(selected, file.Path)
	}
	return selected, skipped, nil
}

func prepare(ctx context.Context, store *cas.Store, events []Event, sequence uint64, request Request, path string, previous *manifest.TrackedFile, before image) (Change, image, error) {
	change := Change{Path: path, Before: previous}
	after := before
	file := manifest.TrackedFile{Path: path}
	if previous != nil {
		file = *previous
		change.Revision = currentRevision(events, path, file.Hash).sequence
	}
	if before.Exists {
		change.BackupHash, change.BackupMode = before.Hash, before.Mode
	}
	switch request.Operation {
	case Track:
		if !before.Exists {
			return change, after, fmt.Errorf("cannot track missing file %s", path)
		}
		file.Immutable, file.RecoverURL = request.Immutable, request.RecoverURL
		if !request.NoHash {
			file.Hash = before.Hash
		}
	case Untrack:
		return change, after, nil
	case Update:
		if request.FromSource {
			var err error
			after, err = sourceImage(ctx, store, file, true, false)
			if err != nil {
				return change, after, err
			}
			if before.Exists {
				after.Mode = before.Mode
			}
		} else if !before.Exists {
			return change, after, fmt.Errorf("cannot accept missing file %s", path)
		}
		file.Hash = after.Hash
	case Recover:
		if file.Hash == "" {
			return change, after, fmt.Errorf("file %s has no accepted hash to recover", path)
		}
		var err error
		after, err = sourceImage(ctx, store, file, false, false)
		if err != nil {
			return change, after, err
		}
		if revision := currentRevision(events, path, file.Hash); revision.mode != 0 {
			after.Mode = uint32(revision.mode.Perm())
		} else if before.Exists {
			after.Mode = before.Mode
		}
	case Revert:
		if file.Hash == "" {
			return change, after, fmt.Errorf("file %s has no accepted revision", path)
		}
		revision, err := revertRevision(events, path, file.Hash, request.Hash, request.Revision)
		if err != nil {
			return change, after, err
		}
		if err := verifyObject(store, revision.hash); err != nil {
			return change, after, fmt.Errorf("historical backup unavailable for %s: %w", path, err)
		}
		file.Hash = revision.hash
		after = image{Exists: true, Hash: revision.hash, Mode: uint32(revision.mode.Perm())}
		if after.Mode == 0 {
			after.Mode = 0o600
		}
		change.Revision = revision.sequence
	case Lock:
		if !before.Exists {
			return change, after, fmt.Errorf("cannot lock missing file %s", path)
		}
		file.Immutable, file.Hash = true, before.Hash
	case Unlock:
		file.Immutable = false
	}
	change.After = &file
	if (request.Operation == Track || request.Operation == Update || request.Operation == Lock) && file.Hash != "" {
		change.Parent = change.Revision
		change.Revision = sequence
		change.Accepted = true
		change.Mode = after.Mode
	}
	return change, after, nil
}

func verifyObject(store *cas.Store, hash string) (resultErr error) {
	digest, err := cas.ParseDigest(hash)
	if err != nil {
		return err
	}
	reader, err := store.Open(digest)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, reader.Close()) }()
	_, err = io.Copy(io.Discard, reader)
	return err
}

// recoveryTimeout bounds one recovery download end to end, so a source that
// accepts the connection and stops responding cannot hold up an installation.
const recoveryTimeout = 5 * time.Minute

// maxRecoveryBytes bounds one recovery download. Tracked files are working
// files, so a longer stream is a misconfigured or hostile source.
const maxRecoveryBytes = 64 << 20

// recoveryClient carries the timeout for declared recovery sources.
var recoveryClient = &http.Client{Timeout: recoveryTimeout}

// cappedReader fails a stream that exceeds its byte budget. Truncating instead
// would store short content, which commits as an object when no accepted digest
// pins the contents.
type cappedReader struct {
	reader io.Reader
	left   int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return n, fmt.Errorf("stream exceeds %d bytes", maxRecoveryBytes)
	}
	return n, err
}

func sourceImage(ctx context.Context, store *cas.Store, file manifest.TrackedFile, update, offline bool) (result image, resultErr error) {
	if !update {
		if err := verifyObject(store, file.Hash); err == nil {
			return image{Exists: true, Hash: file.Hash, Mode: 0o600}, nil
		}
	}
	if offline || file.RecoverURL == "" {
		return result, fmt.Errorf("no verified local backup or recovery source for %s", file.Path)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, file.RecoverURL, nil)
	if err != nil {
		return result, err
	}
	response, err := recoveryClient.Do(request)
	if err != nil {
		return result, fmt.Errorf("fetch recovery source for %s: %w", file.Path, err)
	}
	defer func() { resultErr = errors.Join(resultErr, response.Body.Close()) }()
	if response.StatusCode != http.StatusOK {
		return result, fmt.Errorf("recovery source for %s returned HTTP %d", file.Path, response.StatusCode)
	}
	if response.ContentLength > maxRecoveryBytes {
		return result, fmt.Errorf(
			"recovery source for %s declares %d bytes, over the %d byte limit",
			file.Path,
			response.ContentLength,
			maxRecoveryBytes,
		)
	}
	var expected *cas.Digest
	if !update {
		digest, err := cas.ParseDigest(file.Hash)
		if err != nil {
			return result, err
		}
		expected = &digest
	}
	object, err := store.Put(
		&cappedReader{reader: response.Body, left: maxRecoveryBytes},
		expected,
	)
	if err != nil {
		return result, fmt.Errorf("invalid recovery contents for %s: %w", file.Path, err)
	}
	return image{Exists: true, Hash: object.Digest.String(), Mode: 0o600}, nil
}

// ValidateOwnership includes generated outputs retained by the runtime receipt.
func ValidateOwnership(root string, files []manifest.TrackedFile, locked *lockfile.Document) error {
	if len(files) == 0 {
		return nil
	}
	outputs := []string{}
	add := func(directory, name string) {
		outputs = append(outputs, strings.ToLower(filepath.ToSlash(filepath.Join(directory, name))))
	}
	receiptName := ".lucy/install.yaml"
	if err := workspacewrite.SafePath(root, receiptName); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(root, receiptName))
	if err == nil {
		var receipt struct {
			Entries []struct {
				Path string `yaml:"path"`
			} `yaml:"entries"`
		}
		if err := yaml.Unmarshal(raw, &receipt); err != nil {
			return err
		}
		for _, entry := range receipt.Entries {
			add(".", entry.Path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if locked != nil {
		server := locked.Server
		switch server.Bootstrapper {
		case "", "paperclip":
			add(server.Directory, "server.jar")
		case "fabric":
			for _, name := range []string{"run.sh", "run.bat", "fabric-server-launch.jar", "fabric-server-launcher.properties", "libraries/net/fabricmc/fabric-loader/" + string(server.CoreVersion) + "/fabric-loader-" + string(server.CoreVersion) + ".jar"} {
				add(server.Directory, name)
			}
		case "forge-installer":
			add(server.Directory, "run.sh")
			add(server.Directory, "run.bat")
		}
		for _, input := range server.Inputs {
			add(server.Directory, input.Path)
		}
		if locked.MCDR != nil {
			add(locked.MCDR.Directory, "run-mcdr.sh")
		}
		for _, pkg := range locked.Packages {
			if !pkg.Enabled() {
				continue
			}
			directory, folder := server.Directory, "mods"
			if pkg.Ecosystem == catalog.EcosystemBukkit || pkg.Ecosystem == catalog.EcosystemPaper {
				folder = "plugins"
			}
			if pkg.Environment == serverpkg.EnvironmentMCDR {
				if locked.MCDR == nil {
					return fmt.Errorf("MCDR package has no wrapper runtime")
				}
				directory, folder = locked.MCDR.Directory, "plugins"
			}
			add(directory, filepath.Join(folder, pkg.Filename))
		}
	}
	for _, file := range files {
		path := strings.ToLower(file.Path)
		for _, output := range outputs {
			if path == output || strings.HasPrefix(path, output+"/") || strings.HasPrefix(output, path+"/") {
				return fmt.Errorf("tracked file %s collides with managed runtime output %s", file.Path, output)
			}
		}
	}
	return nil
}
