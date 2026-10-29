package telemetry

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type recordType string

const (
	recordTypeEvent  recordType = payloadTypeEvent
	recordTypeReport recordType = payloadTypeReport
)

// queuedRecord is one durable outbox entry. The payload is stored verbatim so
// retries send identical bytes with the same record ID.
type queuedRecord struct {
	SchemaVersion int            `json:"schema_version"`
	ID            string         `json:"id"`
	Type          recordType     `json:"type"`
	CreatedAt     time.Time      `json:"created_at"`
	BodyBytes     int64          `json:"body_bytes,omitempty"`
	Payload       jsontext.Value `json:"payload"`
}

type payloadResult struct {
	Payload   any
	BodyBytes int64
	Truncated bool
}

type payloadBuilder func(state *persistedState, id string, now time.Time) (payloadResult, error)

func (c *Client) enqueue(ctx context.Context, kind recordType, build payloadBuilder) (Submission, error) {
	release, err := c.enterOperation(ctx, min(c.limits.LockWait, enqueueLockWait))
	if err != nil {
		if ignoreLockBusy(err) {
			return Submission{Reason: SkipBusy}, nil
		}
		return Submission{}, err
	}
	defer release()

	now := c.now()
	state, _, err := c.loadState(now)
	if err != nil {
		return Submission{}, err
	}
	enabled, _, reason := c.effective(state)
	if !enabled {
		return Submission{Reason: reason}, nil
	}
	state.ensureIdentity(now)
	id := NewUUID()
	built, err := build(state, id, now)
	if err != nil {
		return Submission{}, err
	}
	data, err := marshalCanonical(built.Payload)
	if err != nil {
		return Submission{}, fmt.Errorf("telemetry: encode record: %w", err)
	}
	if kind == recordTypeEvent && len(data) > c.limits.MaxEventPayloadBytes {
		return Submission{}, fmt.Errorf("%w: payload is %d bytes, limit %d", ErrInvalidEvent, len(data), c.limits.MaxEventPayloadBytes)
	}
	data, bodyBytes, truncated, err := c.fitRecord(kind, id, built.Payload, data, built.BodyBytes, built.Truncated)
	if err != nil {
		return Submission{}, err
	}
	if kind == recordTypeReport && bodyBytes == 0 {
		return Submission{}, fmt.Errorf("%w: body is empty after record fitting", ErrInvalidReport)
	}
	if int64(len(data)) > c.limits.MaxQueuedBytes {
		return Submission{Reason: SkipQueueFull}, nil
	}
	record := queuedRecord{
		SchemaVersion: SchemaVersion,
		ID:            id,
		Type:          kind,
		CreatedAt:     now.UTC(),
		BodyBytes:     bodyBytes,
		Payload:       jsontext.Value(data),
	}
	if err := c.writeQueueRecord(record); err != nil {
		return Submission{}, err
	}
	state.Queue = append(state.Queue, queueEntry{
		ID:           id,
		Type:         kind,
		Sequence:     state.NextSequence,
		CreatedAt:    now.UTC(),
		PayloadBytes: int64(len(data)),
		BodyBytes:    bodyBytes,
	})
	state.NextSequence++
	removed := c.pruneQueue(state, now)
	if !queueContains(state.Queue, id) {
		_ = os.Remove(c.queueRecordPath(id))
		return Submission{Reason: SkipQueueFull}, nil
	}
	if err := c.saveState(state, now); err != nil {
		_ = os.Remove(c.queueRecordPath(id))
		return Submission{}, err
	}
	c.deleteQueuePayloads(removed)
	c.pruneQueueOrphans(state)
	return Submission{
		Queued:        true,
		RecordID:      id,
		Truncated:     truncated,
		QueuedRecords: len(state.Queue),
	}, nil
}

// fitRecord trims a report body until the complete serialized wire record fits
// the server's per-record bound. Event payloads are already bounded strictly
// enough that they must fit without adjustment.
func (c *Client) fitRecord(kind recordType, id string, payload any, data []byte, bodyBytes int64, truncated bool) ([]byte, int64, bool, error) {
	for range 8 {
		encoded, err := marshalCanonical(wireRecord{ID: id, Type: kind, Payload: jsontext.Value(data)})
		if err != nil {
			return nil, 0, truncated, fmt.Errorf("telemetry: encode record envelope: %w", err)
		}
		if len(encoded) <= c.limits.MaxRecordBytes {
			return data, bodyBytes, truncated, nil
		}
		report, ok := payload.(reportPayload)
		if !ok {
			return nil, 0, truncated, fmt.Errorf("%w: serialized record is %d bytes, limit %d", ErrInvalidEvent, len(encoded), c.limits.MaxRecordBytes)
		}
		overflow := len(encoded) - c.limits.MaxRecordBytes
		newLength := len(report.Body) - overflow - 32
		if newLength <= 0 {
			return nil, 0, truncated, fmt.Errorf("%w: record metadata exceeds %d bytes", ErrInvalidReport, c.limits.MaxRecordBytes)
		}
		report.Body = truncateUTF8(report.Body, min(newLength, len(report.Body)-1))
		payload = report
		data, err = marshalCanonical(report)
		if err != nil {
			return nil, 0, truncated, fmt.Errorf("telemetry: encode report: %w", err)
		}
		bodyBytes = int64(len(report.Body))
		truncated = true
	}
	return nil, 0, truncated, fmt.Errorf("%w: report does not fit the record bound", ErrInvalidReport)
}

func (c *Client) writeQueueRecord(record queuedRecord) error {
	data, err := marshalCanonical(record)
	if err != nil {
		return fmt.Errorf("telemetry: encode outbox record: %w", err)
	}
	if err := writeFileAtomic(c.queueRecordPath(record.ID), data); err != nil {
		return fmt.Errorf("telemetry: write outbox record: %w", err)
	}
	return nil
}

func (c *Client) readQueueRecord(id string) (queuedRecord, error) {
	data, err := os.ReadFile(c.queueRecordPath(id))
	if err != nil {
		return queuedRecord{}, err
	}
	var record queuedRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return queuedRecord{}, fmt.Errorf("decode outbox record %s: %w", id, err)
	}
	if record.ID != id || (record.Type != recordTypeEvent && record.Type != recordTypeReport) || len(record.Payload) == 0 {
		return queuedRecord{}, fmt.Errorf("outbox record %s is inconsistent", id)
	}
	return record, nil
}

func (c *Client) deleteQueuePayloads(entries []queueEntry) {
	for _, entry := range entries {
		if err := os.Remove(c.queueRecordPath(entry.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			c.debug("telemetry: remove outbox record failed", "id", entry.ID, "error", err)
		}
	}
}

func (c *Client) finishDisabledPurge(state *persistedState, now time.Time) error {
	if err := os.RemoveAll(c.queueDirectory()); err != nil {
		return fmt.Errorf("telemetry is off, but pending records could not be deleted: %w", err)
	}
	state.Queue = nil
	state.QueueBytes = 0
	state.PurgePending = false
	return c.saveState(state, now)
}

// pruneQueue removes stale records first, then oldest records until both queue
// bounds hold. The record just enqueued has the highest sequence and survives
// unless a single record exceeds MaxQueuedBytes (checked by the caller).
func (c *Client) pruneQueue(state *persistedState, now time.Time) []queueEntry {
	cutoff := now.Add(-c.limits.MaxRecordAge)
	removed := make([]queueEntry, 0)
	kept := state.Queue[:0]
	var totalBytes int64
	for _, entry := range state.Queue {
		if entry.CreatedAt.Before(cutoff) {
			removed = append(removed, entry)
			continue
		}
		kept = append(kept, entry)
		totalBytes += entry.PayloadBytes
	}
	state.Queue = kept
	for len(state.Queue) > 0 && (len(state.Queue) > c.limits.MaxQueuedRecords || totalBytes > c.limits.MaxQueuedBytes) {
		entry := state.Queue[0]
		state.Queue = state.Queue[1:]
		totalBytes -= entry.PayloadBytes
		removed = append(removed, entry)
	}
	state.QueueBytes = totalBytes
	if len(removed) > 0 {
		state.Dropped.Records += int64(len(removed))
		for _, entry := range removed {
			state.Dropped.Bytes += entry.PayloadBytes
		}
	}
	return removed
}

func (c *Client) pruneQueueOrphans(state *persistedState) {
	entries, err := os.ReadDir(c.queueDirectory())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.debug("telemetry: read outbox directory failed", "error", err)
		}
		return
	}
	known := make(map[string]struct{}, len(state.Queue))
	for _, entry := range state.Queue {
		known[entry.ID] = struct{}{}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(c.queueDirectory(), name)
		if strings.HasPrefix(name, ".") {
			_ = os.Remove(path)
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if _, exists := known[id]; exists {
			continue
		}
		_ = os.Remove(path)
	}
}

func queueContains(queue []queueEntry, id string) bool {
	for _, entry := range queue {
		if entry.ID == id {
			return true
		}
	}
	return false
}

func queueStatistics(state *persistedState) QueueStats {
	stats := QueueStats{
		Records:      len(state.Queue),
		PayloadBytes: state.QueueBytes,
	}
	for _, entry := range state.Queue {
		switch entry.Type {
		case recordTypeEvent:
			stats.Events++
		case recordTypeReport:
			stats.Reports++
			stats.ReportBodyBytes += entry.BodyBytes
		}
		if stats.Oldest.IsZero() || entry.CreatedAt.Before(stats.Oldest) {
			stats.Oldest = entry.CreatedAt
		}
	}
	return stats
}
