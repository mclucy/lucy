package telemetry

import (
	"errors"
	"slices"
	"time"
)

// Schema and protocol values that appear on the wire.
const (
	payloadTypeEvent  = "event"
	payloadTypeReport = "report"
)

// ErrInvalidEvent reports a caller-provided event that does not satisfy the
// telemetry schema. It is a programming error, not a telemetry outage.
var ErrInvalidEvent = errors.New("telemetry: invalid event")

// ErrInvalidReport reports a caller-provided report that does not satisfy the
// telemetry schema. It is a programming error, not a telemetry outage.
var ErrInvalidReport = errors.New("telemetry: invalid report")

// CollectionPolicy is the persisted reason telemetry is enabled or disabled.
// The wire value of a record is default or opt_in; opt_out records are never
// collected.
type CollectionPolicy string

const (
	// CollectionDefault follows the channel default (on for alpha and beta).
	CollectionDefault CollectionPolicy = "default"
	// CollectionOptIn is an explicit user enablement.
	CollectionOptIn CollectionPolicy = "opt_in"
	// CollectionOptOut is an explicit user disablement.
	CollectionOptOut CollectionPolicy = "opt_out"
)

// SkipReason explains why a record was not queued or an upload did not run.
type SkipReason string

const (
	SkipNone        SkipReason = ""
	SkipDisabled    SkipReason = "disabled"
	SkipEnvironment SkipReason = "environment_disabled"
	SkipEndpoint    SkipReason = "endpoint_unconfigured"
	SkipCooldown    SkipReason = "cooldown"
	SkipBusy        SkipReason = "busy"
	SkipEmpty       SkipReason = "empty"
	SkipQueueFull   SkipReason = "queue_full"
)

// Outcome is the result of one Lucy invocation.
type Outcome string

const (
	OutcomeSuccess  Outcome = "success"
	OutcomeFailure  Outcome = "failure"
	OutcomeCanceled Outcome = "canceled"
)

// ContextStatus describes whether a typed context snapshot was available.
type ContextStatus string

const (
	ContextPresent ContextStatus = "present"
	ContextAbsent  ContextStatus = "absent"
	ContextFailed  ContextStatus = "failed"
)

// ContextKind names a typed context snapshot. The wire type is a bounded
// string; these constants name the kinds Lucy currently emits.
type ContextKind string

const (
	ContextServer     ContextKind = "server"
	ContextRuntime    ContextKind = "runtime"
	ContextManifest   ContextKind = "manifest"
	ContextLockfile   ContextKind = "lockfile"
	ContextPackages   ContextKind = "packages"
	ContextWorkspace  ContextKind = "workspace"
	ContextInvocation ContextKind = "invocation"
	ContextExecution  ContextKind = "execution"
	ContextPrompt     ContextKind = "prompt"
	ContextScale      ContextKind = "scale"
	ContextWarnings   ContextKind = "warnings"
	ContextCollection ContextKind = "collection"
)

// ContextReason explains why a typed snapshot is absent or failed. It is a
// bounded wire value; unrecognized reasons are rejected before enqueueing.
type ContextReason string

const (
	ContextReasonNotObserved       ContextReason = "not_observed"
	ContextReasonNotReached        ContextReason = "not_reached"
	ContextReasonNotApplicable     ContextReason = "not_applicable"
	ContextReasonNoResult          ContextReason = "no_result"
	ContextReasonObservationFailed ContextReason = "observation_failed"
)

// Context is an optional typed snapshot attached to an event. Data must be
// JSON-marshalable and fit in 8 KiB. Data is required when Status is present
// and must be nil when Status is absent or failed. Reason is optional for
// absent contexts (the default is not_observed) and failed contexts (the
// default is observation_failed), and must be empty for present contexts.
// Version is stamped as 1 by the writer; zero selects the current version.
type Context struct {
	Kind    ContextKind
	Status  ContextStatus
	Version int
	Reason  ContextReason
	Data    any
}

// Event describes one non-sensitive invocation observation. Operation is the
// trusted normalized command path only (for example "java list"); Event
// excludes arguments and their values.
type Event struct {
	InvocationID string
	Operation    string
	Duration     time.Duration
	// DurationMeasured emits duration_ms even when Duration is zero. A
	// positive Duration is emitted regardless; an unmeasured zero is omitted.
	DurationMeasured bool
	Outcome          Outcome
	ErrorCode        string
	OccurredAt       time.Time
	Contexts         []Context
}

// ReportKind classifies an explicitly submitted report.
type ReportKind string

const (
	ReportFailure       ReportKind = "failure"
	ReportPanic         ReportKind = "panic"
	ReportAgentFeedback ReportKind = "agent_feedback"
)

// Report is a free-form, explicitly submitted diagnostic. Body is sanitized,
// bounded UTF-8 text with common secrets, IP addresses, and absolute paths
// redacted as defense in depth, not anonymization. Callers must not place raw
// transcripts or file attachments in Body. Agent and Model are optional
// bounded identifiers.
type Report struct {
	Kind         ReportKind
	InvocationID string
	Agent        string
	Model        string
	Body         string
	CreatedAt    time.Time
}

// Submission reports whether a record was durably placed in the local queue.
// Queued means the record will be retried later; it does not mean the server
// has accepted it. When Queued is false, Reason explains why.
type Submission struct {
	Queued        bool       `json:"queued"`
	RecordID      string     `json:"record_id,omitempty"`
	Reason        SkipReason `json:"reason,omitempty"`
	Truncated     bool       `json:"truncated,omitempty"`
	QueuedRecords int        `json:"queued_records"`
}

// FlushResult describes one bounded upload run. AcceptedIDs and RejectedIDs
// contain record IDs the server permanently accepted or rejected in this run,
// so a caller can check the fate of a specific report's Submission.RecordID.
// Details are diagnostic text with no effect on CLI outcomes.
type FlushResult struct {
	Attempted   int        `json:"attempted"`
	Accepted    int        `json:"accepted"`
	Rejected    int        `json:"rejected"`
	Remaining   int        `json:"remaining"`
	Skipped     bool       `json:"skipped"`
	Reason      SkipReason `json:"reason,omitempty"`
	Detail      string     `json:"detail,omitempty"`
	AcceptedIDs []string   `json:"accepted_ids,omitempty"`
	RejectedIDs []string   `json:"rejected_ids,omitempty"`
}

// WasAccepted reports whether the server durably accepted id during this run.
func (result FlushResult) WasAccepted(id string) bool {
	return slices.Contains(result.AcceptedIDs, id)
}

// WasRejected reports whether the server permanently rejected id during this
// run. Rejected records are removed from the queue without a retry.
func (result FlushResult) WasRejected(id string) bool {
	return slices.Contains(result.RejectedIDs, id)
}

// AcceptedStats counts records the server durably accepted in the current
// statistics window. Bytes cover logical payload and sanitized body lengths
// and exclude HTTP, TLS, and framing overhead.
type AcceptedStats struct {
	Events            int64 `json:"events"`
	Reports           int64 `json:"reports"`
	Records           int64 `json:"records"`
	EventPayloadBytes int64 `json:"event_payload_bytes"`
	ReportBodyBytes   int64 `json:"report_body_bytes"`
}

// RejectedStats counts records the server permanently rejected in the current
// statistics window. Bytes are logical serialized payload bytes.
type RejectedStats struct {
	Records      int64 `json:"records"`
	PayloadBytes int64 `json:"payload_bytes"`
}

// DroppedStats counts records discarded locally because they were stale,
// corrupt, or exceeded the bounded queue.
type DroppedStats struct {
	Records int64 `json:"records"`
	Bytes   int64 `json:"bytes"`
}

// TransportStats counts locally persisted upload attempts. Requests, outcome
// counts, and RequestBodyBytes cover every HTTP attempt, including retries.
// RequestBodyBytes is the exact serialized JSON body handed to the HTTP
// transport; it excludes HTTP headers, framing, TLS, and other wire overhead.
// Uncertain counts attempts whose acceptance is unknown (transport error,
// timeout, or an unparseable success response).
// Throttles counts attempts a retryable response identified as intake-limited
// (HTTP 429, or a rejection reason of intake_limited) and counts denied
// delivery attempts by attempt. An attempt interrupted by process termination
// may be missing from these counters.
type TransportStats struct {
	Requests             int64 `json:"requests"`
	Succeeded            int64 `json:"succeeded"`
	RejectedRequests     int64 `json:"rejected_requests"`
	Uncertain            int64 `json:"uncertain"`
	Throttles            int64 `json:"throttles"`
	RequestBodyBytes     int64 `json:"request_body_bytes"`
	LastRequestBodyBytes int64 `json:"last_request_body_bytes"`
}

// QueueStats describes locally queued records. PayloadBytes and ReportBodyBytes
// are logical lengths.
type QueueStats struct {
	Records         int       `json:"records"`
	Events          int       `json:"events"`
	Reports         int       `json:"reports"`
	PayloadBytes    int64     `json:"payload_bytes"`
	ReportBodyBytes int64     `json:"report_body_bytes"`
	Oldest          time.Time `json:"oldest,omitzero"`
}

// Stats is a transparent snapshot of telemetry state. Enabled reports whether
// collection is allowed; EndpointConfigured separately reports whether uploads
// can run. PeriodStart begins at identity creation or the last ResetIdentity;
// pending records from before a reset are accepted later and count in the new
// window.
type Stats struct {
	Enabled            bool             `json:"enabled"`
	Reason             SkipReason       `json:"reason,omitempty"`
	Policy             CollectionPolicy `json:"policy"`
	Endpoint           string           `json:"endpoint,omitempty"`
	EndpointConfigured bool             `json:"endpoint_configured"`
	InstallationID     string           `json:"installation_id,omitempty"`
	PeriodStart        time.Time        `json:"period_start,omitzero"`
	Accepted           AcceptedStats    `json:"accepted"`
	Rejected           RejectedStats    `json:"rejected"`
	Dropped            DroppedStats     `json:"dropped"`
	Transport          TransportStats   `json:"transport"`
	Queue              QueueStats       `json:"queue"`
	CooldownUntil      time.Time        `json:"cooldown_until,omitzero"`
	LastError          string           `json:"last_error,omitempty"`
	StateDirectory     string           `json:"state_directory,omitempty"`
}

// Identity is the result of ResetIdentity.
type Identity struct {
	InstallationID string           `json:"installation_id"`
	CreatedAt      time.Time        `json:"created_at"`
	Policy         CollectionPolicy `json:"policy"`
}
