package telemetry

import (
	"cmp"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

var errCorruptState = errors.New("telemetry: state is corrupt; run `lucy telemetry off` to reset it")

type acceptedState struct {
	Events            int64 `json:"events"`
	Reports           int64 `json:"reports"`
	EventPayloadBytes int64 `json:"event_payload_bytes"`
	ReportBodyBytes   int64 `json:"report_body_bytes"`
}

type rejectedState struct {
	Records      int64 `json:"records"`
	PayloadBytes int64 `json:"payload_bytes"`
}

type droppedState struct {
	Records int64 `json:"records"`
	Bytes   int64 `json:"bytes"`
}

type transportState struct {
	Requests             int64 `json:"requests"`
	Succeeded            int64 `json:"succeeded"`
	RejectedRequests     int64 `json:"rejected_requests"`
	Uncertain            int64 `json:"uncertain"`
	RequestBodyBytes     int64 `json:"request_body_bytes"`
	LastRequestBodyBytes int64 `json:"last_request_body_bytes"`
}

type queueEntry struct {
	ID           string     `json:"id"`
	Type         recordType `json:"type"`
	Sequence     int64      `json:"sequence"`
	CreatedAt    time.Time  `json:"created_at"`
	PayloadBytes int64      `json:"payload_bytes"`
	BodyBytes    int64      `json:"body_bytes,omitempty"`
}

// persistedState is the single source of truth for identity, policy, counters,
// and the queue index. Queue payloads live in queue/<id>.json so accepting a
// record and counting it happen in one atomic state replacement.
type persistedState struct {
	SchemaVersion     int              `json:"schema_version"`
	InstallationID    string           `json:"installation_id,omitempty"`
	IdentityCreatedAt time.Time        `json:"identity_created_at,omitzero"`
	Policy            CollectionPolicy `json:"policy"`
	PeriodStart       time.Time        `json:"period_start,omitzero"`
	Accepted          acceptedState    `json:"accepted"`
	Rejected          rejectedState    `json:"rejected"`
	Dropped           droppedState     `json:"dropped"`
	Transport         transportState   `json:"transport"`
	PurgePending      bool             `json:"purge_pending,omitzero"`
	Queue             []queueEntry     `json:"queue,omitempty"`
	QueueBytes        int64            `json:"queue_bytes"`
	NextSequence      int64            `json:"next_sequence"`
	AcceptedIDs       []string         `json:"accepted_ids,omitempty"`
	CooldownUntil     time.Time        `json:"cooldown_until,omitzero"`
	LastError         string           `json:"last_error,omitempty"`
	LastUploadAt      time.Time        `json:"last_upload_at,omitzero"`
	UpdatedAt         time.Time        `json:"updated_at,omitzero"`
}

func newState(now time.Time) *persistedState {
	return &persistedState{
		SchemaVersion: SchemaVersion,
		Policy:        CollectionDefault,
		PeriodStart:   now.UTC(),
		NextSequence:  1,
	}
}

func (c *Client) statePath() string {
	return filepath.Join(c.directory, "state.json")
}

func (c *Client) lockPath() string {
	return filepath.Join(c.directory, "state.lock")
}

func (c *Client) queueDirectory() string {
	return filepath.Join(c.directory, "queue")
}

func (c *Client) queueRecordPath(id string) string {
	return filepath.Join(c.queueDirectory(), id+".json")
}

func (c *Client) ensureDirectories() error {
	for _, directory := range []string{c.directory, c.queueDirectory()} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("telemetry: create state directory: %w", err)
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("telemetry: secure state directory: %w", err)
		}
	}
	return nil
}

func (c *Client) loadState(now time.Time) (*persistedState, bool, error) {
	data, err := os.ReadFile(c.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return newState(now), false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("telemetry: read state: %w", err)
	}
	_ = os.Chmod(c.statePath(), 0o600)
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, true, fmt.Errorf("%w: %v", errCorruptState, err)
	}
	if state.SchemaVersion < 1 {
		return nil, true, errCorruptState
	}
	switch state.Policy {
	case CollectionDefault, CollectionOptIn, CollectionOptOut:
	default:
		return nil, true, errCorruptState
	}
	if state.SchemaVersion > SchemaVersion {
		return nil, true, fmt.Errorf("telemetry: state schema %d is newer than supported schema %d", state.SchemaVersion, SchemaVersion)
	}
	if state.InstallationID != "" {
		if _, err := normalizeUUID(state.InstallationID); err != nil {
			return nil, true, errCorruptState
		}
	}
	state.normalize(now)
	if state.PurgePending {
		if state.Policy != CollectionOptOut {
			return nil, true, errCorruptState
		}
		if err := c.finishDisabledPurge(&state, now); err != nil {
			return nil, true, err
		}
	}
	return &state, true, nil
}

func (c *Client) saveState(state *persistedState, now time.Time) error {
	state.SchemaVersion = SchemaVersion
	state.UpdatedAt = now.UTC()
	data, err := marshalCanonical(state)
	if err != nil {
		return fmt.Errorf("telemetry: encode state: %w", err)
	}
	if err := writeFileAtomic(c.statePath(), data); err != nil {
		return fmt.Errorf("telemetry: write state: %w", err)
	}
	return nil
}

func (state *persistedState) normalize(now time.Time) {
	if state.InstallationID != "" {
		installationID, err := normalizeUUID(state.InstallationID)
		if err != nil {
			state.InstallationID = ""
			state.IdentityCreatedAt = time.Time{}
		} else {
			state.InstallationID = installationID
		}
	}
	if state.PeriodStart.IsZero() {
		state.PeriodStart = now.UTC()
	}
	if state.InstallationID != "" && state.IdentityCreatedAt.IsZero() {
		state.IdentityCreatedAt = state.PeriodStart
	}
	if state.InstallationID == "" {
		state.IdentityCreatedAt = time.Time{}
	}
	if state.NextSequence < 1 {
		state.NextSequence = 1
	}
	kept := state.Queue[:0]
	var queueBytes int64
	var maximumSequence int64
	for _, entry := range state.Queue {
		entryID, err := normalizeUUID(entry.ID)
		if err != nil || entry.PayloadBytes <= 0 {
			state.Dropped.Records++
			continue
		}
		if entry.Type != recordTypeEvent && entry.Type != recordTypeReport {
			state.Dropped.Records++
			continue
		}
		if entry.Sequence < 1 {
			state.Dropped.Records++
			continue
		}
		entry.ID = entryID
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = now.UTC()
		}
		kept = append(kept, entry)
		queueBytes += entry.PayloadBytes
		maximumSequence = max(maximumSequence, entry.Sequence)
	}
	state.Queue = kept
	state.QueueBytes = queueBytes
	if state.NextSequence <= maximumSequence {
		state.NextSequence = maximumSequence + 1
	}
	slices.SortFunc(state.Queue, func(left, right queueEntry) int {
		return cmp.Compare(left.Sequence, right.Sequence)
	})
	state.normalizeAcceptedIDs()
}

func (state *persistedState) normalizeAcceptedIDs() {
	if len(state.AcceptedIDs) == 0 {
		state.AcceptedIDs = nil
		return
	}
	seen := make(map[string]struct{}, len(state.AcceptedIDs))
	kept := make([]string, 0, min(len(state.AcceptedIDs), defaultMaxAcceptedIDHistory))
	start := max(0, len(state.AcceptedIDs)-defaultMaxAcceptedIDHistory)
	for _, raw := range state.AcceptedIDs[start:] {
		id, err := normalizeUUID(raw)
		if err != nil {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		kept = append(kept, id)
	}
	state.AcceptedIDs = kept
}

func (state *persistedState) rememberAccepted(id string) bool {
	for _, accepted := range state.AcceptedIDs {
		if accepted == id {
			return false
		}
	}
	state.AcceptedIDs = append(state.AcceptedIDs, id)
	if len(state.AcceptedIDs) > defaultMaxAcceptedIDHistory {
		state.AcceptedIDs = state.AcceptedIDs[len(state.AcceptedIDs)-defaultMaxAcceptedIDHistory:]
	}
	return true
}

// countDropped adds records discarded locally without a server decision.
func (state *persistedState) countDropped(entries []queueEntry) {
	state.Dropped.Records += int64(len(entries))
	for _, entry := range entries {
		state.Dropped.Bytes += entry.PayloadBytes
	}
}

// countRejected adds one record the server permanently rejected.
func (state *persistedState) countRejected(payloadBytes int64) {
	state.Rejected.Records++
	state.Rejected.PayloadBytes += payloadBytes
}

func (state *persistedState) ensureIdentity(now time.Time) {
	if state.InstallationID != "" {
		return
	}
	state.InstallationID = NewUUID()
	state.IdentityCreatedAt = now.UTC()
	if state.Accepted == (acceptedState{}) && state.Rejected == (rejectedState{}) && state.Dropped == (droppedState{}) {
		state.PeriodStart = now.UTC()
	}
}

// clearBackoff forgets a pending retry deadline and its diagnostic. Explicit
// policy changes and identity resets supersede an earlier transport failure.
func (state *persistedState) clearBackoff() {
	state.CooldownUntil = time.Time{}
	state.LastError = ""
}

// deferRetry records a failed attempt and blocks uploads until the cooldown
// elapses. A failure that must not defer retries records LastError directly.
func (state *persistedState) deferRetry(now time.Time, cooldown time.Duration, message string) {
	state.CooldownUntil = now.Add(cooldown)
	state.LastError = message
}

// optIn records an explicit opt-in and creates an identity when none exists.
func (state *persistedState) optIn(now time.Time) {
	state.Policy = CollectionOptIn
	state.ensureIdentity(now)
	state.clearBackoff()
}

// optOut records an explicit opt-out. PurgePending makes the queue purge
// resumable; the queue index is cleared only after its payloads are gone.
func (state *persistedState) optOut() {
	state.Policy = CollectionOptOut
	state.PurgePending = true
	state.clearBackoff()
}

// completePurge clears the queue index and the purge marker after the payload
// directory has been removed.
func (state *persistedState) completePurge() {
	state.Queue = nil
	state.QueueBytes = 0
	state.PurgePending = false
}

// resetIdentity rotates the installation UUID and restarts the statistics
// window. Queued records keep their IDs and payloads.
func (state *persistedState) resetIdentity(now time.Time) {
	state.InstallationID = NewUUID()
	state.IdentityCreatedAt = now.UTC()
	state.PeriodStart = now.UTC()
	state.Accepted = acceptedState{}
	state.Rejected = rejectedState{}
	state.Dropped = droppedState{}
	state.Transport = transportState{}
	state.AcceptedIDs = nil
	state.clearBackoff()
	state.LastUploadAt = time.Time{}
}

func marshalCanonical(value any) ([]byte, error) {
	return json.Marshal(value, json.Deterministic(true))
}

func writeFileAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() {
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	temporaryPath = ""
	syncDirectory(directory)
	return nil
}

func syncDirectory(path string) {
	directory, err := os.Open(path)
	if err != nil {
		return
	}
	_ = directory.Sync()
	_ = directory.Close()
}
