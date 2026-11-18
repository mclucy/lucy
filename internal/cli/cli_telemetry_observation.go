package cli

import (
	"context"
	"sync"
	"time"

	"github.com/mclucy/lucy/internal/telemetry"
	"github.com/mclucy/lucy/log"
)

// Bounds shared by every observation projection. They mirror the wire
// contract and are applied before data leaves the process.
const (
	maxTelemetryElapsedMS   = 604800000
	maxTelemetryPhaseCount  = 24
	maxTelemetryScale       = 1000000
	maxTelemetryPromptCount = 10000
	maxTelemetryWarnings    = 10000
	slowInvocationThreshold = 30 * time.Second
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
// durations are not exclusive time.
type TelemetryExecution struct {
	Phases       []TelemetryPhase `json:"phases,omitempty"`
	ActivePhase  string           `json:"active_phase,omitempty"`
	CancelReason string           `json:"cancel_reason,omitempty"`
	FailureKind  string           `json:"failure_kind,omitempty"`
	Slow         bool             `json:"slow"`
	HTTPMS       *int64           `json:"http_ms,omitempty"`
	LockMS       *int64           `json:"lock_ms,omitempty"`
}

// TelemetryPrompt is the bounded prompt snapshot of one invocation. Aborted
// means the user explicitly aborted a form; Interrupted means the surrounding
// context was canceled while a form was open. Neither carries typed input.
type TelemetryPrompt struct {
	Mode        string `json:"mode"`
	Count       int    `json:"count"`
	WaitMS      int64  `json:"wait_ms"`
	Aborted     bool   `json:"aborted"`
	Interrupted bool   `json:"interrupted,omitzero"`
}

// telemetryScale accumulates counts by value. A negative field is unobserved;
// zero is a real observed zero. Optional wire pointers are built only when
// projecting the context.
type telemetryScale struct {
	Packages          int
	RequestedPackages int
	Files             int
	CompletedFiles    int
}

// telemetryScaleData is the bounded wire projection of telemetryScale. Files
// counts files written by a phase; CompletedFiles counts completed file
// transfers. They are separate metrics and may differ.
type telemetryScaleData struct {
	Packages          *int `json:"packages,omitempty"`
	RequestedPackages *int `json:"requested_packages,omitempty"`
	Files             *int `json:"files,omitempty"`
	CompletedFiles    *int `json:"completed_files,omitempty"`
}

func newTelemetryScale() telemetryScale {
	return telemetryScale{
		Packages:          -1,
		RequestedPackages: -1,
		Files:             -1,
		CompletedFiles:    -1,
	}
}

func (scale *telemetryScale) observeMax(target *int, value int) {
	if value < 0 {
		return
	}
	value = min(value, maxTelemetryScale)
	if *target < value {
		*target = value
	}
}

func (scale *telemetryScale) increment(target *int) {
	value := 0
	if *target > 0 {
		value = *target
	}
	*target = min(value+1, maxTelemetryScale)
}

func (scale telemetryScale) any() bool {
	return scale.Packages >= 0 ||
		scale.RequestedPackages >= 0 ||
		scale.Files >= 0 ||
		scale.CompletedFiles >= 0
}

func (scale telemetryScale) data() telemetryScaleData {
	return telemetryScaleData{
		Packages:          telemetryCountPointer(scale.Packages),
		RequestedPackages: telemetryCountPointer(scale.RequestedPackages),
		Files:             telemetryCountPointer(scale.Files),
		CompletedFiles:    telemetryCountPointer(scale.CompletedFiles),
	}
}

func telemetryCountPointer(value int) *int {
	if value < 0 {
		return nil
	}
	pointer := value
	return &pointer
}

type telemetryWarnings struct {
	Recoverable          int  `json:"recoverable"`
	ResolutionRejections *int `json:"resolution_rejections,omitempty"`
}

// TelemetryObservation accumulates the bounded facts one invocation observed
// about itself. It is independent of rendering: hooks may run on any
// goroutine, and every method is safe on a nil receiver so a disabled
// invocation allocates and records nothing.
type TelemetryObservation struct {
	mu       sync.Mutex
	started  time.Time
	logger   *log.Logger
	phases   []TelemetryPhase
	active   string
	failure  string
	cancel   string
	prompt   TelemetryPrompt
	scale    telemetryScale
	warnings int
	rejects  int
}

// NewTelemetryObservation starts one observation. Callers create it only when
// telemetry is enabled for the invocation.
func NewTelemetryObservation() *TelemetryObservation {
	return &TelemetryObservation{
		started: time.Now(),
		prompt:  TelemetryPrompt{Mode: "unknown"},
		scale:   newTelemetryScale(),
	}
}

type telemetryObservationKey struct{}

// WithTelemetryObservation returns a context that carries obs to shared
// helpers such as RunPrompt. A nil observation leaves ctx unchanged.
func WithTelemetryObservation(ctx context.Context, obs *TelemetryObservation) context.Context {
	if obs == nil {
		return ctx
	}
	return context.WithValue(ctx, telemetryObservationKey{}, obs)
}

func telemetryObservationFromContext(ctx context.Context) *TelemetryObservation {
	if ctx == nil {
		return nil
	}
	observation, _ := ctx.Value(telemetryObservationKey{}).(*TelemetryObservation)
	return observation
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

// SetFailureKind records the bounded failure class. Unknown values are
// ignored rather than passed through.
func (o *TelemetryObservation) SetFailureKind(kind string) {
	if o == nil || !allowedTelemetryValue(kind, "usage", "panic", "handled", "command") {
		return
	}
	o.mu.Lock()
	o.failure = kind
	o.mu.Unlock()
}

// SetCancelReason records the bounded cancellation class. Unknown values are
// ignored rather than passed through.
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

// SetPromptMode records whether prompting was available. Unknown values
// degrade to unknown instead of surfacing a free-form string.
func (o *TelemetryObservation) SetPromptMode(mode string) {
	if o == nil {
		return
	}
	if !allowedTelemetryValue(mode, "available", "non_interactive", "no_terminal", "unknown") {
		mode = "unknown"
	}
	o.mu.Lock()
	o.prompt.Mode = mode
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

// PhaseStart records that a bounded phase began. The entry stays active until
// PhaseDone reports it, so a failed or canceled trace keeps the unfinished
// phase visible. Unknown names are ignored.
func (o *TelemetryObservation) PhaseStart(name string) {
	if o == nil {
		return
	}
	id, ok := telemetryPhaseID(name)
	if !ok {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if index := o.phaseIndex(id); index >= 0 {
		if o.phases[index].State != "active" {
			o.phases[index].started = time.Now()
		}
		o.phases[index].State = "active"
	} else if len(o.phases) < maxTelemetryPhaseCount {
		o.phases = append(o.phases, TelemetryPhase{
			Name:    id,
			State:   "active",
			started: time.Now(),
		})
	}
	o.active = id
}

// PhaseDone records the observed elapsed time of a phase. Repeated completions
// of the same phase accumulate, up to the wire bound.
func (o *TelemetryObservation) PhaseDone(name string, elapsed time.Duration) {
	if o == nil {
		return
	}
	id, ok := telemetryPhaseID(name)
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

// ObservePhaseCount projects bounded counts that a finished phase reported.
// Only phases whose counters are meaningful contribute.
func (o *TelemetryObservation) ObservePhaseCount(name string, count, total int) {
	if o == nil {
		return
	}
	id, ok := telemetryPhaseID(name)
	if !ok {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	switch id {
	case "packages":
		o.scale.observeMax(&o.scale.Packages, count)
		o.scale.observeMax(&o.scale.RequestedPackages, total)
	case "files":
		o.scale.observeMax(&o.scale.Files, count)
	}
}

// ObserveFetchDone records one completed file transfer without its name.
func (o *TelemetryObservation) ObserveFetchDone() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.scale.increment(&o.scale.CompletedFiles)
}

// ObserveInputScale records declared and locked package counts before
// resolution completes. A negative value means the caller did not observe it;
// zero is a real observed count. No names or versions are retained.
func (o *TelemetryObservation) ObserveInputScale(requestedPackages, lockedPackages int) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if requestedPackages >= 0 {
		o.scale.observeMax(&o.scale.RequestedPackages, requestedPackages)
	}
	if lockedPackages >= 0 {
		o.scale.observeMax(&o.scale.Packages, lockedPackages)
	}
}

// ObserveResolveRejected records one rejected candidate decision. Neither its
// name nor its reason is retained. Retry branches may report the same
// candidate again, so this is an observed rejection count, not a unique total.
func (o *TelemetryObservation) ObserveResolveRejected() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.rejects < maxTelemetryWarnings {
		o.rejects++
	}
}

// ObserveWarning counts one recoverable warning already routed to the user.
func (o *TelemetryObservation) ObserveWarning() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.warnings < maxTelemetryWarnings {
		o.warnings++
	}
}

// ObservePromptWait records one prompt interaction and whether the user
// explicitly aborted it or the surrounding context was canceled while it was
// open. Callers pass an abort only when the prompt itself reported it and the
// context was not canceled.
func (o *TelemetryObservation) ObservePromptWait(elapsed time.Duration, aborted, interrupted bool) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.prompt.Count < maxTelemetryPromptCount {
		o.prompt.Count++
	}
	o.prompt.WaitMS = min(
		o.prompt.WaitMS+telemetryMilliseconds(elapsed),
		maxTelemetryElapsedMS,
	)
	if aborted {
		o.prompt.Aborted = true
	}
	if interrupted {
		o.prompt.Interrupted = true
	}
}

// PromptAborted reports whether a prompt was explicitly aborted.
func (o *TelemetryObservation) PromptAborted() bool {
	if o == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.prompt.Aborted
}

// Contexts projects the execution, prompt, scale, and warning contexts. It is
// called once at the end of an invocation; the returned values own their data.
func (o *TelemetryObservation) Contexts(ended time.Time) (
	execution telemetry.Context,
	prompt telemetry.Context,
	scale telemetry.Context,
	warnings telemetry.Context,
) {
	if o == nil {
		return telemetry.Context{}, telemetry.Context{}, telemetry.Context{}, telemetry.Context{}
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
	executionData := TelemetryExecution{
		Phases:       phases,
		ActivePhase:  o.active,
		CancelReason: o.cancel,
		FailureKind:  o.failure,
		Slow:         o.slow(ended),
	}
	scaleData := o.scale.data()
	scalePresent := o.scale.any()
	warningsData := telemetryWarnings{Recoverable: o.warnings}
	if o.rejects > 0 {
		rejections := o.rejects
		warningsData.ResolutionRejections = &rejections
	}
	promptData := o.prompt
	o.mu.Unlock()

	if logger != nil {
		http, lock := logger.WaitTimings()
		executionData.HTTPMS = durationMilliseconds(http)
		executionData.LockMS = durationMilliseconds(lock)
	}

	execution = telemetry.Context{
		Kind:   "execution",
		Status: telemetry.ContextPresent,
		Data:   executionData,
	}
	prompt = telemetry.Context{
		Kind:   "prompt",
		Status: telemetry.ContextPresent,
		Data:   promptData,
	}
	if scalePresent {
		scale = telemetry.Context{
			Kind:   "scale",
			Status: telemetry.ContextPresent,
			Data:   scaleData,
		}
	} else {
		scale = telemetry.Context{
			Kind:   "scale",
			Status: telemetry.ContextAbsent,
			Reason: telemetry.ContextReasonNotObserved,
		}
	}
	warnings = telemetry.Context{
		Kind:   "warnings",
		Status: telemetry.ContextPresent,
		Data:   warningsData,
	}
	return execution, prompt, scale, warnings
}

func (o *TelemetryObservation) slow(ended time.Time) bool {
	if o.started.IsZero() || ended.IsZero() {
		return false
	}
	return ended.Sub(o.started) >= slowInvocationThreshold
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

var telemetryPhaseIDs = map[string]string{
	"setup":              "setup",
	"command":            "command",
	"server":             "server",
	"packages":           "packages",
	"install":            "install",
	"python environment": "python_environment",
	"python_environment": "python_environment",
	"download pack":      "download_pack",
	"download_pack":      "download_pack",
	"download archive":   "download_archive",
	"download_archive":   "download_archive",
	"files":              "files",
	"loader installer":   "loader_installer",
	"loader_installer":   "loader_installer",
	"adoption":           "adoption",
	"identification":     "identification",
}

func telemetryPhaseID(name string) (string, bool) {
	id, ok := telemetryPhaseIDs[name]
	return id, ok
}
