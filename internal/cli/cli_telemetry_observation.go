package cli

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/internal/telemetry"
	"github.com/mclucy/lucy/log"
)

// Bounds shared by every observation projection. They mirror the wire
// contract and are applied before data leaves the process.
const (
	maxTelemetryElapsedMS  = 604800000
	maxTelemetryPhaseCount = 24
	maxTelemetryPhaseName  = 64
	maxTelemetryScale      = 1000000
)

// TelemetryPhase is one phase of an invocation. Elapsed is observed time
// only; a phase that never reported completion stays active.
type TelemetryPhase struct {
	Name      string `json:"name"`
	ElapsedMS int64  `json:"elapsed_ms"`
	State     string `json:"state"`

	// started is the phase's own start, used only to measure an unfinished
	// phase at projection time. It is never serialized.
	started time.Time
}

// TelemetryExecution is the bounded execution snapshot of one invocation.
// Phases may overlap and an active phase means unfinished work, so the
// durations are not exclusive time. InputWaitMS is present only when a form
// actually ran, so an unmeasured wait differs from a measured zero.
type TelemetryExecution struct {
	Phases       []TelemetryPhase `json:"phases,omitempty"`
	ActivePhase  string           `json:"active_phase,omitempty"`
	CancelReason string           `json:"cancel_reason,omitempty"`
	FailureKind  string           `json:"failure_kind,omitempty"`
	InputWaitMS  *int64           `json:"input_wait_ms,omitempty"`
	Files        *int             `json:"files,omitempty"`
	HTTPMS       *int64           `json:"http_ms,omitempty"`
	LockMS       *int64           `json:"lock_ms,omitempty"`
}

// TelemetryObservation accumulates the bounded facts one invocation observed
// about itself. It is independent of rendering: hooks may run on any
// goroutine, and every method is safe on a nil receiver so a disabled
// invocation allocates and records nothing.
type TelemetryObservation struct {
	mu          sync.Mutex
	logger      *log.Logger
	phases      []TelemetryPhase
	active      string
	failure     string
	cancel      string
	promptTimer PromptTimer
	files       int
	packages    int
}

// NewTelemetryObservation starts one observation. Callers create it only when
// telemetry is enabled for the invocation.
func NewTelemetryObservation() *TelemetryObservation {
	return &TelemetryObservation{
		files:    -1,
		packages: -1,
	}
}

// WithTelemetryObservation attaches the observation's input timer.
func WithTelemetryObservation(ctx context.Context, obs *TelemetryObservation) context.Context {
	if obs == nil {
		return ctx
	}
	return WithPromptTimer(ctx, &obs.promptTimer)
}

// AttachLogger lets the observation read bounded wait samples at projection
// time. A nil logger leaves the wait fields unmeasured.
func (o *TelemetryObservation) AttachLogger(logger *log.Logger) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.logger = logger
	o.mu.Unlock()
}

// SetFailureKind records the bounded failure class.
func (o *TelemetryObservation) SetFailureKind(kind string) {
	if o == nil || !allowedTelemetryValue(kind, "usage", "panic", "handled", "command") {
		return
	}
	o.mu.Lock()
	o.failure = kind
	o.mu.Unlock()
}

// SetCancelReason records the bounded cancellation class.
func (o *TelemetryObservation) SetCancelReason(reason string) {
	if o == nil || !allowedTelemetryValue(
		reason,
		"prompt_aborted", "interrupted", "deadline", "context_canceled",
	) {
		return
	}
	o.mu.Lock()
	o.cancel = reason
	o.mu.Unlock()
}

func allowedTelemetryValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// PhaseStart records that a code-defined phase began. The entry stays active
// until PhaseDone reports it, so a failed or canceled trace keeps the
// unfinished phase visible. A phase beyond the projection cap leaves the
// previous active phase in place instead of naming an entry that is absent.
func (o *TelemetryObservation) PhaseStart(name string) {
	if o == nil {
		return
	}
	id, ok := telemetryPhaseName(name)
	if !ok {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	index := o.phaseIndex(id)
	switch {
	case index >= 0:
		if o.phases[index].State != "active" {
			o.phases[index].started = time.Now()
		}
		o.phases[index].State = "active"
		o.active = id
	case len(o.phases) < maxTelemetryPhaseCount:
		o.phases = append(o.phases, TelemetryPhase{
			Name:    id,
			State:   "active",
			started: time.Now(),
		})
		o.active = id
	}
}

// PhaseDone records the observed elapsed time of a phase. Repeated completions
// of the same phase accumulate, up to the wire bound.
func (o *TelemetryObservation) PhaseDone(name string, elapsed time.Duration) {
	if o == nil {
		return
	}
	id, ok := telemetryPhaseName(name)
	if !ok {
		return
	}
	milliseconds := telemetryMilliseconds(elapsed)
	o.mu.Lock()
	defer o.mu.Unlock()
	index := o.phaseIndex(id)
	if index < 0 {
		if len(o.phases) >= maxTelemetryPhaseCount {
			return
		}
		o.phases = append(o.phases, TelemetryPhase{Name: id, State: "complete"})
		index = len(o.phases) - 1
	}
	phase := &o.phases[index]
	phase.ElapsedMS = min(phase.ElapsedMS+milliseconds, maxTelemetryElapsedMS)
	phase.State = "complete"
	phase.started = time.Time{}
	if o.active == id {
		o.active = ""
		for index := len(o.phases) - 1; index >= 0; index-- {
			if o.phases[index].State == "active" {
				o.active = o.phases[index].Name
				break
			}
		}
	}
}

// ObserveFiles records the largest meaningful file count a finished phase
// reported. A negative count is unobserved; zero is a real observed count.
func (o *TelemetryObservation) ObserveFiles(count int) {
	if o == nil || count < 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.files < count {
		o.files = min(count, maxTelemetryScale)
	}
}

// ObservePackages records the largest meaningful package count the run
// observed. A negative count is unobserved; zero is a real observed count.
func (o *TelemetryObservation) ObservePackages(count int) {
	if o == nil || count < 0 {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.packages < count {
		o.packages = min(count, maxTelemetryScale)
	}
}

// Packages projects the observed package count for the workspace context. It
// is nil when no phase observed a count, so an empty workspace is distinct
// from one that was never inspected.
func (o *TelemetryObservation) Packages() *int {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return telemetryCountPointer(o.packages)
}

func telemetryCountPointer(value int) *int {
	if value < 0 {
		return nil
	}
	pointer := value
	return &pointer
}

// Execution projects the bounded execution context. It is called once at the
// end of an invocation; the returned value owns its data.
func (o *TelemetryObservation) Execution(ended time.Time) telemetry.Context {
	if o == nil {
		return telemetry.Context{
			Kind:   "execution",
			Status: telemetry.ContextAbsent,
			Reason: telemetry.ContextReasonNotReached,
		}
	}
	o.mu.Lock()
	logger := o.logger
	phases := make([]TelemetryPhase, len(o.phases))
	copy(phases, o.phases)
	for index := range phases {
		if phases[index].State != "active" ||
			phases[index].started.IsZero() ||
			ended.IsZero() ||
			!ended.After(phases[index].started) {
			continue
		}
		active := telemetryMilliseconds(ended.Sub(phases[index].started))
		phases[index].ElapsedMS = min(
			phases[index].ElapsedMS+active,
			maxTelemetryElapsedMS,
		)
	}
	data := TelemetryExecution{
		Phases:       phases,
		ActivePhase:  o.active,
		CancelReason: o.cancel,
		FailureKind:  o.failure,
		Files:        telemetryCountPointer(o.files),
	}
	o.mu.Unlock()

	if elapsed, measured := o.promptTimer.wait(); measured {
		milliseconds := telemetryMilliseconds(elapsed)
		data.InputWaitMS = &milliseconds
	}
	if logger != nil {
		http, lock := logger.WaitTimings()
		data.HTTPMS = durationMilliseconds(http)
		data.LockMS = durationMilliseconds(lock)
	}
	return telemetry.Context{
		Kind:   "execution",
		Status: telemetry.ContextPresent,
		Data:   data,
	}
}

func (o *TelemetryObservation) phaseIndex(id string) int {
	for index := range o.phases {
		if o.phases[index].Name == id {
			return index
		}
	}
	return -1
}

func telemetryMilliseconds(elapsed time.Duration) int64 {
	if elapsed <= 0 {
		return 0
	}
	return min(elapsed.Milliseconds(), maxTelemetryElapsedMS)
}

func durationMilliseconds(value *time.Duration) *int64 {
	if value == nil {
		return nil
	}
	milliseconds := telemetryMilliseconds(*value)
	return &milliseconds
}

// telemetryPhaseName turns a code-defined phase label into the bounded code
// name the wire accepts. ASCII spaces become underscores once at this
// boundary; every other character must satisfy [a-z0-9][a-z0-9_-]{0,63}.
func telemetryPhaseName(name string) (string, bool) {
	normalized := strings.ReplaceAll(name, " ", "_")
	if normalized == "" || len(normalized) > maxTelemetryPhaseName {
		return "", false
	}
	for index := range len(normalized) {
		character := normalized[index]
		if character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' {
			continue
		}
		if index > 0 && (character == '_' || character == '-') {
			continue
		}
		return "", false
	}
	return normalized, true
}
