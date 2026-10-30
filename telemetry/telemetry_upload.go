package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type preparedRecord struct {
	entry  queueEntry
	record queuedRecord
	wire   wireRecord
}

// Flush performs one bounded upload run. It never blocks on another process's
// upload: a busy state returns Skipped/busy and leaves the queue intact.
// Network, health, and protocol failures are reported through FlushResult and
// never as errors. Errors are limited to local state I/O.
func (c *Client) Flush(ctx context.Context) (FlushResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := FlushResult{}
	if c.disabled {
		result.Skipped = true
		result.Reason = SkipDisabled
		return result, nil
	}
	if environmentDisabled() {
		result.Skipped = true
		result.Reason = SkipEnvironment
		return result, nil
	}
	if c.endpoint == "" {
		result.Skipped = true
		result.Reason = SkipEndpoint
		return result, nil
	}
	release, err := c.enterOperation(ctx, 0)
	if err != nil {
		if ignoreLockBusy(err) {
			result.Skipped = true
			result.Reason = SkipBusy
			return result, nil
		}
		return result, err
	}
	defer release()

	flushContext, cancel := context.WithTimeout(ctx, c.limits.FlushTimeout)
	defer cancel()
	now := c.now()
	state, _, err := c.loadState(now)
	if err != nil {
		return result, err
	}
	enabled, _, reason := c.effective(state)
	if !enabled {
		result.Skipped = true
		result.Reason = reason
		return result, nil
	}
	removed := c.pruneQueue(state, now)
	if len(state.Queue) == 0 {
		if len(removed) > 0 {
			if err := c.saveState(state, now); err != nil {
				return result, err
			}
			c.deleteQueuePayloads(removed)
		}
		result.Skipped = true
		result.Reason = SkipEmpty
		return result, nil
	}
	if !state.CooldownUntil.IsZero() && now.Before(state.CooldownUntil) {
		if err := c.saveState(state, now); err != nil {
			return result, err
		}
		c.deleteQueuePayloads(removed)
		result.Skipped = true
		result.Reason = SkipCooldown
		result.Detail = "retry after " + state.CooldownUntil.UTC().Format(time.RFC3339)
		result.Remaining = len(state.Queue)
		return result, nil
	}
	if ctx.Err() != nil {
		return result, nil
	}
	if err := c.checkHealth(flushContext); err != nil {
		if errors.Is(flushContext.Err(), context.Canceled) {
			return result, nil
		}
		state.CooldownUntil = now.Add(c.limits.Cooldown)
		state.LastError = "health check: " + sanitizeErrorText(err)
		if saveErr := c.saveState(state, now); saveErr != nil {
			return result, saveErr
		}
		c.deleteQueuePayloads(removed)
		c.pruneQueueOrphans(state)
		result.Skipped = true
		result.Reason = SkipCooldown
		result.Detail = state.LastError
		result.Remaining = len(state.Queue)
		return result, nil
	}
	state.CooldownUntil = time.Time{}
	skipped := make(map[string]struct{})
	for range c.limits.MaxBatches {
		if flushContext.Err() != nil {
			break
		}
		prepared, missing := c.prepareBatch(state, skipped)
		if len(missing) > 0 {
			c.dropEntries(state, missing)
		}
		if len(prepared) == 0 {
			if err := c.saveState(state, now); err != nil {
				return result, err
			}
			break
		}
		records := make([]wireRecord, len(prepared))
		for index := range prepared {
			records[index] = prepared[index].wire
		}
		upload, uploadErr := c.uploadRecords(flushContext, records, state)
		result.Attempted += len(records)
		if uploadErr != nil {
			if failure, isHTTP := errors.AsType[*httpError](uploadErr); isHTTP && failure.batchInvalid {
				rejectedEntries := c.applyRejectedBatch(state, prepared)
				result.Rejected += len(rejectedEntries)
				result.RejectedIDs = append(result.RejectedIDs, entryIDs(rejectedEntries)...)
				state.LastError = sanitizeErrorText(uploadErr)
				if err := c.saveState(state, now); err != nil {
					return result, err
				}
				c.deleteQueuePayloads(rejectedEntries)
				continue
			}
			state.LastError = sanitizeErrorText(uploadErr)
			if !errors.Is(flushContext.Err(), context.Canceled) {
				state.CooldownUntil = now.Add(c.limits.Cooldown)
			}
			if err := c.saveState(state, now); err != nil {
				return result, err
			}
			result.Remaining = len(state.Queue)
			result.Detail = state.LastError
			return result, nil
		}
		acceptedEntries, rejectedEntries := c.applyUploadResult(state, prepared, upload, skipped, now)
		result.Accepted += len(acceptedEntries)
		result.Rejected += len(rejectedEntries)
		result.AcceptedIDs = append(result.AcceptedIDs, entryIDs(acceptedEntries)...)
		result.RejectedIDs = append(result.RejectedIDs, entryIDs(rejectedEntries)...)
		state.LastError = ""
		if err := c.saveState(state, now); err != nil {
			return result, err
		}
		c.deleteQueuePayloads(acceptedEntries)
		c.deleteQueuePayloads(rejectedEntries)
		classified := len(acceptedEntries) + len(rejectedEntries)
		if classified == 0 {
			result.Detail = "server did not acknowledge any record in the batch"
			break
		}
		if classified < len(prepared) {
			result.Detail = fmt.Sprintf("server classified %d of %d records in the batch", classified, len(prepared))
			break
		}
	}
	if err := c.saveState(state, now); err != nil {
		return result, err
	}
	result.Remaining = len(state.Queue)
	if result.Attempted == 0 && len(state.Queue) == 0 {
		result.Skipped = true
		result.Reason = SkipEmpty
	}
	c.pruneQueueOrphans(state)
	return result, nil
}

// prepareBatch loads the oldest records that fit both server bounds. Records
// whose outbox files are missing or corrupt are returned for local dropping so
// they cannot block healthy records.
func (c *Client) prepareBatch(state *persistedState, skipped map[string]struct{}) ([]preparedRecord, []queueEntry) {
	prepared := make([]preparedRecord, 0, min(len(state.Queue), c.limits.MaxRecordsPerRequest))
	missing := make([]queueEntry, 0)
	estimated := int64(len(`{"records":[]}`))
	for _, entry := range state.Queue {
		if _, skip := skipped[entry.ID]; skip {
			continue
		}
		if len(prepared) >= c.limits.MaxRecordsPerRequest {
			break
		}
		record, err := c.readQueueRecord(entry.ID)
		if err != nil {
			c.debug("telemetry: dropping unreadable outbox record", "id", entry.ID, "error", err)
			missing = append(missing, entry)
			continue
		}
		wire := wireRecord{ID: entry.ID, Type: entry.Type, Payload: record.Payload}
		encoded, err := marshalCanonical(wire)
		if err != nil || len(encoded) > c.limits.MaxRecordBytes {
			missing = append(missing, entry)
			continue
		}
		additional := int64(len(encoded)) + 1
		if len(prepared) > 0 && estimated+additional > int64(c.limits.MaxRequestBytes) {
			break
		}
		estimated += additional
		prepared = append(prepared, preparedRecord{entry: entry, record: record, wire: wire})
	}
	return prepared, missing
}

func (c *Client) dropEntries(state *persistedState, entries []queueEntry) {
	if len(entries) == 0 {
		return
	}
	drop := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		drop[entry.ID] = struct{}{}
	}
	c.removeQueueIDs(state, drop)
	for _, entry := range entries {
		state.Dropped.Records++
		state.Dropped.Bytes += entry.PayloadBytes
	}
}

func (c *Client) applyUploadResult(state *persistedState, prepared []preparedRecord, upload uploadResult, skipped map[string]struct{}, now time.Time) ([]queueEntry, []queueEntry) {
	acceptedIDs := make(map[string]struct{}, len(upload.accepted))
	for _, id := range upload.accepted {
		acceptedIDs[id] = struct{}{}
	}
	rejectedIDs := make(map[string]struct{}, len(upload.rejected))
	for _, rejection := range upload.rejected {
		rejectedIDs[rejection.ID] = struct{}{}
	}
	accepted := make([]queueEntry, 0)
	rejected := make([]queueEntry, 0)
	remove := make(map[string]struct{})
	for _, item := range prepared {
		if _, ok := acceptedIDs[item.entry.ID]; ok {
			delete(rejectedIDs, item.entry.ID)
			if _, duplicate := remove[item.entry.ID]; duplicate {
				continue
			}
			remove[item.entry.ID] = struct{}{}
			accepted = append(accepted, item.entry)
			if !state.rememberAccepted(item.entry.ID) {
				continue
			}
			switch item.entry.Type {
			case recordTypeEvent:
				state.Accepted.Events++
				state.Accepted.EventPayloadBytes += int64(len(item.record.Payload))
			case recordTypeReport:
				state.Accepted.Reports++
				state.Accepted.ReportBodyBytes += item.entry.BodyBytes
			}
			continue
		}
		if _, ok := rejectedIDs[item.entry.ID]; ok {
			remove[item.entry.ID] = struct{}{}
			rejected = append(rejected, item.entry)
			state.Rejected.Records++
			state.Rejected.PayloadBytes += int64(len(item.record.Payload))
			continue
		}
		skipped[item.entry.ID] = struct{}{}
	}
	c.removeQueueIDs(state, remove)
	state.LastUploadAt = now.UTC()
	return accepted, rejected
}

func (c *Client) applyRejectedBatch(state *persistedState, prepared []preparedRecord) []queueEntry {
	remove := make(map[string]struct{}, len(prepared))
	rejected := make([]queueEntry, 0, len(prepared))
	for _, item := range prepared {
		remove[item.entry.ID] = struct{}{}
		rejected = append(rejected, item.entry)
		state.Rejected.Records++
		state.Rejected.PayloadBytes += int64(len(item.record.Payload))
	}
	c.removeQueueIDs(state, remove)
	return rejected
}

func (c *Client) removeQueueIDs(state *persistedState, remove map[string]struct{}) {
	if len(remove) == 0 {
		return
	}
	kept := state.Queue[:0]
	var queueBytes int64
	for _, entry := range state.Queue {
		if _, drop := remove[entry.ID]; drop {
			continue
		}
		kept = append(kept, entry)
		queueBytes += entry.PayloadBytes
	}
	state.Queue = kept
	state.QueueBytes = queueBytes
}

func entryIDs(entries []queueEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids
}
