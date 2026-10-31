package telemetry

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Client is a concurrency-safe telemetry client. All state lives beneath its
// private directory. Create one client per process.
type Client struct {
	disabled  bool
	directory string
	endpoint  string
	channel   string
	version   string
	commit    string
	os        string
	arch      string
	logger    Logger
	http      *http.Client
	ownsHTTP  bool
	now       func() time.Time
	limits    Limits
	sem       chan struct{}
}

// New validates config and builds a client. It performs no disk or network
// I/O; the first event or explicit operation initializes state lazily.
func New(config Config) (*Client, error) {
	config, err := config.withDefaults()
	if err != nil {
		return nil, err
	}
	httpClient := config.HTTPClient
	ownsHTTP := false
	if httpClient == nil {
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			httpClient = &http.Client{
				Transport:     transport.Clone(),
				Timeout:       config.Limits.RequestTimeout,
				CheckRedirect: safeRedirect,
			}
		} else {
			httpClient = &http.Client{Timeout: config.Limits.RequestTimeout, CheckRedirect: safeRedirect}
		}
		ownsHTTP = true
	} else {
		cloned := *httpClient
		previous := cloned.CheckRedirect
		cloned.CheckRedirect = func(request *http.Request, via []*http.Request) error {
			if err := safeRedirect(request, via); err != nil {
				return err
			}
			if previous != nil {
				return previous(request, via)
			}
			return nil
		}
		httpClient = &cloned
	}
	return &Client{
		disabled:  config.Disabled,
		directory: config.Directory,
		endpoint:  config.Endpoint,
		channel:   config.Channel,
		version:   config.LucyVersion,
		commit:    config.LucyCommit,
		os:        config.OS,
		arch:      config.Arch,
		logger:    config.Logger,
		http:      httpClient,
		ownsHTTP:  ownsHTTP,
		now:       config.Now,
		limits:    config.Limits,
		sem:       make(chan struct{}, 1),
	}, nil
}

// Close releases idle HTTP connections owned by the client. It never touches
// telemetry state.
func (c *Client) Close() error {
	if c.ownsHTTP && c.http != nil {
		c.http.CloseIdleConnections()
	}
	return nil
}

// Enabled reports whether collection is currently allowed. It does not report
// whether an endpoint is configured; Flush handles that separately. An empty
// endpoint still permits durable local enqueueing.
func (c *Client) Enabled(ctx context.Context) (bool, error) {
	if c.disabled || environmentDisabled() {
		return false, nil
	}
	release, err := c.enterOperation(ctx, c.limits.LockWait)
	if err != nil {
		return false, err
	}
	defer release()
	state, _, err := c.loadState(c.now())
	if err != nil {
		return false, err
	}
	enabled, _, _ := c.effective(state)
	return enabled, nil
}

// InstallationID returns the persistent installation UUID, creating it on
// first startup even when telemetry is disabled.
func (c *Client) InstallationID(ctx context.Context) (string, error) {
	release, err := c.enterOperation(ctx, c.limits.LockWait)
	if err != nil {
		return "", err
	}
	defer release()
	now := c.now()
	state, _, err := c.loadState(now)
	if err != nil {
		return "", err
	}
	if state.InstallationID != "" {
		return state.InstallationID, nil
	}
	state.ensureIdentity(now)
	if err := c.saveState(state, now); err != nil {
		return "", err
	}
	return state.InstallationID, nil
}

// RecordEvent records one invocation observation. It never uploads; call
// Flush separately when networking is allowed. Callers own InvocationID.
func (c *Client) RecordEvent(ctx context.Context, event Event) (Submission, error) {
	return c.enqueue(ctx, recordTypeEvent, func(state *persistedState, _ string, now time.Time) (payloadResult, error) {
		prepared, err := c.prepareEvent(event, now)
		if err != nil {
			return payloadResult{}, err
		}
		return payloadResult{Payload: c.buildEventPayload(prepared, state)}, nil
	})
}

// SubmitReport records one explicitly supplied report. It sanitizes and bounds
// the body, never uploads, and never enables telemetry. When telemetry is off,
// it returns a non-error Submission whose Reason explains the refusal; the
// body is not inspected in that case.
func (c *Client) SubmitReport(ctx context.Context, report Report) (Submission, error) {
	return c.enqueue(ctx, recordTypeReport, func(state *persistedState, id string, now time.Time) (payloadResult, error) {
		prepared, truncated, err := c.prepareReport(report, now)
		if err != nil {
			return payloadResult{}, err
		}
		return payloadResult{
			Payload: reportPayload{
				ReportID:         id,
				InstallationID:   state.InstallationID,
				InvocationID:     prepared.invocationID,
				Kind:             string(prepared.kind),
				Agent:            prepared.agent,
				Model:            prepared.model,
				CreatedAt:        prepared.createdAt.Format(time.RFC3339),
				SchemaVersion:    SchemaVersion,
				CollectionPolicy: payloadPolicy(state.Policy),
				Body:             prepared.body,
			},
			BodyBytes: int64(len(prepared.body)),
			Truncated: truncated,
		}, nil
	})
}

// SetEnabled persists an explicit opt-in or opt-out. Disabling purges queued
// records without rotating the identity and waits for any in-flight upload in
// this process or another cooperating process. Enabling creates an identity if
// one does not exist. It does not upload.
func (c *Client) SetEnabled(ctx context.Context, enabled bool) error {
	release, err := c.enterOperation(ctx, -1)
	if err != nil {
		return err
	}
	defer release()
	now := c.now()
	state, _, err := c.loadState(now)
	if err != nil {
		if enabled || !errors.Is(err, errCorruptState) {
			return err
		}
		state = newState(now)
	}
	if enabled {
		state.optIn(now)
	} else {
		state.optOut()
	}
	if err := c.saveState(state, now); err != nil {
		return err
	}
	if !enabled {
		return c.finishDisabledPurge(state, now)
	}
	return nil
}

// ResetIdentity rotates the installation UUID and restarts the statistics
// window. Pending records keep their IDs and payloads and may be accepted
// later; they count in the new window. Remote data is never deleted.
func (c *Client) ResetIdentity(ctx context.Context) (Identity, error) {
	release, err := c.enterOperation(ctx, -1)
	if err != nil {
		return Identity{}, err
	}
	defer release()
	now := c.now()
	state, _, err := c.loadState(now)
	if err != nil {
		return Identity{}, err
	}
	state.resetIdentity(now)
	if err := c.saveState(state, now); err != nil {
		return Identity{}, err
	}
	return Identity{
		InstallationID: state.InstallationID,
		CreatedAt:      state.IdentityCreatedAt,
		Policy:         state.Policy,
	}, nil
}

// Stats returns a transparent snapshot. Reading statistics may initialize a
// default state document but never creates an installation identity or sends
// anything.
func (c *Client) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{
		Policy:             CollectionDefault,
		Endpoint:           c.endpoint,
		EndpointConfigured: c.endpoint != "",
		StateDirectory:     c.directory,
	}
	release, err := c.enterOperation(ctx, c.limits.LockWait)
	if err != nil {
		return stats, err
	}
	defer release()
	state, existed, err := c.loadState(c.now())
	if err != nil {
		return stats, err
	}
	enabled, policy, reason := c.effective(state)
	stats.Enabled = enabled
	stats.Reason = reason
	stats.Policy = policy
	stats.InstallationID = state.InstallationID
	if existed {
		stats.PeriodStart = state.PeriodStart
	}
	stats.Accepted = AcceptedStats{
		Events:            state.Accepted.Events,
		Reports:           state.Accepted.Reports,
		Records:           state.Accepted.Events + state.Accepted.Reports,
		EventPayloadBytes: state.Accepted.EventPayloadBytes,
		ReportBodyBytes:   state.Accepted.ReportBodyBytes,
	}
	stats.Rejected = RejectedStats(state.Rejected)
	stats.Dropped = DroppedStats(state.Dropped)
	stats.Transport = TransportStats(state.Transport)
	stats.Queue = queueStatistics(state)
	stats.CooldownUntil = state.CooldownUntil
	stats.LastError = state.LastError
	return stats, nil
}

func (c *Client) effective(state *persistedState) (bool, CollectionPolicy, SkipReason) {
	policy := state.Policy
	if policy == "" {
		policy = CollectionDefault
	}
	if c.disabled {
		return false, policy, SkipDisabled
	}
	if environmentDisabled() {
		return false, policy, SkipEnvironment
	}
	switch policy {
	case CollectionOptOut:
		return false, policy, SkipDisabled
	case CollectionOptIn:
		return true, policy, SkipNone
	default:
		if channelCollectsByDefault(c.channel) {
			return true, policy, SkipNone
		}
		return false, policy, SkipDisabled
	}
}

func payloadPolicy(policy CollectionPolicy) string {
	if policy == CollectionOptIn {
		return string(CollectionOptIn)
	}
	return string(CollectionDefault)
}

func (c *Client) debug(message string, fields ...any) {
	if c.logger != nil {
		c.logger.Debug(message, fields...)
	}
}

func (c *Client) warn(err error) {
	if c.logger != nil && err != nil {
		c.logger.Warn(err)
	}
}

func ignoreLockBusy(err error) bool {
	return errors.Is(err, errLockBusy)
}

// safeRedirect keeps telemetry on the configured origin.
func safeRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("telemetry: too many redirects")
	}
	if len(via) > 0 && (!strings.EqualFold(request.URL.Host, via[0].URL.Host) ||
		!strings.EqualFold(request.URL.Scheme, via[0].URL.Scheme)) {
		return errors.New("telemetry: redirect must stay on the configured origin")
	}
	if request.URL.Scheme == "https" {
		return nil
	}
	if request.URL.Scheme == "http" && isLoopbackHost(request.URL.Hostname()) {
		return nil
	}
	return errors.New("telemetry: redirect must use https outside loopback")
}
