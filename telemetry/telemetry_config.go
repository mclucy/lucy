// Package telemetry implements Lucy's opt-out usage telemetry client.
//
// The client owns a private, per-user state directory. It records bounded,
// typed event records and explicit reports into a durable local queue, and
// uploads them only when the caller invokes Flush. It never starts a daemon,
// never inspects files outside its own state directory, and never collects
// process arguments, environment variables, logs, or arbitrary host data.
//
// Telemetry is enabled by default for pre-release channels (alpha and beta).
// LUCY_TELEMETRY can force it off for a process. Persisted opt-in and opt-out
// choices override channel defaults. Disabling telemetry (SetEnabled with
// false) purges queued records without rotating the installation identity.
// Resetting the identity rotates the UUID and restarts the statistics window,
// but keeps queued records and their original identities.
//
// Every error returned by this package is telemetry-local. A CLI must ignore
// it: telemetry failure never changes the result of a regular command.
package telemetry

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// EnvironmentVariable forces telemetry off when set to a false-like value.
// Recognized false values are 0, off, false, no, disabled, opt-out, and
// opt_out. An unrecognized non-empty value also disables telemetry (privacy
// favours failing closed). Recognized true values are 1, on, true, yes,
// enabled, opt-in, and opt_in; they never override a persisted opt-out.
const EnvironmentVariable = "LUCY_TELEMETRY"

// SchemaVersion is the wire and local-state schema version.
const SchemaVersion = 1

// DefaultEndpoint is the public Lucy telemetry ingestion endpoint. Builds may
// override it with a linker flag for a separate deployment.
var DefaultEndpoint = "https://telemetry.lucy.lc"

const (
	stateDirectoryName = "telemetry"
	defaultChannel     = "alpha"

	defaultMaxQueuedRecords     = 512
	defaultMaxQueuedBytes       = 4 << 20
	defaultMaxEventPayloadBytes = 16 << 10
	defaultMaxReportBodyBytes   = 64 << 10
	defaultMaxRecordsPerRequest = 50
	defaultMaxRequestBytes      = 256 << 10
	defaultMaxRecordBytes       = 64 << 10
	defaultMaxAcceptedIDHistory = 2048
	defaultMaxRecordAge         = 7 * 24 * time.Hour
	defaultHealthTimeout        = 2 * time.Second
	defaultRequestTimeout       = 5 * time.Second
	defaultMaxAttempts          = 3
	defaultRetryBackoff         = 250 * time.Millisecond
	defaultCooldown             = 10 * time.Minute
	defaultLockWait             = 2 * time.Second
	defaultMaxBatches           = 4
	defaultFlushTimeout         = 15 * time.Second
	enqueueLockWait             = 250 * time.Millisecond
	truncationMarker            = "\n[truncated]"
)

// Logger is the subset of the Lucy logger used by the client. A *log.Logger
// satisfies it.
type Logger interface {
	Debug(content any, fields ...any)
	Warn(err error)
}

// Limits bounds local collection and network requests. Zero values select the
// default. MaxRecordBytes applies to one serialized wire record, including its
// JSON envelope. MaxRequestBytes applies to one serialized upload request.
type Limits struct {
	MaxQueuedRecords     int
	MaxQueuedBytes       int64
	MaxEventPayloadBytes int
	MaxReportBodyBytes   int
	MaxRecordsPerRequest int
	MaxRequestBytes      int
	MaxRecordBytes       int
	MaxRecordAge         time.Duration
	HealthTimeout        time.Duration
	RequestTimeout       time.Duration
	MaxAttempts          int
	RetryBackoff         time.Duration
	Cooldown             time.Duration
	LockWait             time.Duration
	MaxBatches           int
	FlushTimeout         time.Duration
}

// Config configures a Client. Directory, Endpoint, and Channel default to the
// platform user data directory, DefaultEndpoint, and alpha. Endpoint must use
// HTTPS unless it points at a loopback host, which keeps tests hermetic.
type Config struct {
	Directory   string
	Endpoint    string
	Channel     string
	LucyVersion string
	LucyCommit  string
	OS          string
	Arch        string
	Logger      Logger
	HTTPClient  *http.Client
	Now         func() time.Time
	Limits      Limits
}

func (config Config) withDefaults() (Config, error) {
	if config.Now == nil {
		config.Now = time.Now
	}
	if strings.TrimSpace(config.Directory) == "" {
		directory, err := defaultDirectory()
		if err != nil {
			return Config{}, err
		}
		config.Directory = directory
	}
	config.Directory = filepath.Clean(config.Directory)
	config.Channel = strings.ToLower(strings.TrimSpace(config.Channel))
	if config.Channel == "" {
		config.Channel = defaultChannel
	}
	if !validChannel(config.Channel) {
		return Config{}, fmt.Errorf("telemetry: invalid channel %q", config.Channel)
	}
	config.LucyVersion = sanitizeVersion(config.LucyVersion)
	config.LucyCommit = sanitizeCommit(config.LucyCommit)
	if config.OS == "" {
		config.OS = runtime.GOOS
	}
	if config.Arch == "" {
		config.Arch = runtime.GOARCH
	}
	config.OS = sanitizeToken(config.OS, 32)
	config.Arch = sanitizeToken(config.Arch, 32)
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = strings.TrimSpace(DefaultEndpoint)
	}
	normalizedEndpoint, err := normalizeEndpoint(endpoint)
	if err != nil {
		return Config{}, err
	}
	config.Endpoint = normalizedEndpoint
	config.Limits = config.Limits.withDefaults()
	return config, nil
}

func (limits Limits) withDefaults() Limits {
	if limits.MaxQueuedRecords <= 0 {
		limits.MaxQueuedRecords = defaultMaxQueuedRecords
	}
	if limits.MaxQueuedBytes <= 0 {
		limits.MaxQueuedBytes = defaultMaxQueuedBytes
	}
	if limits.MaxEventPayloadBytes <= 0 {
		limits.MaxEventPayloadBytes = defaultMaxEventPayloadBytes
	}
	if limits.MaxReportBodyBytes <= 0 {
		limits.MaxReportBodyBytes = defaultMaxReportBodyBytes
	}
	if limits.MaxRecordsPerRequest <= 0 {
		limits.MaxRecordsPerRequest = defaultMaxRecordsPerRequest
	}
	if limits.MaxRequestBytes <= 0 {
		limits.MaxRequestBytes = defaultMaxRequestBytes
	}
	if limits.MaxRecordBytes <= 0 {
		limits.MaxRecordBytes = defaultMaxRecordBytes
	}
	if limits.MaxRecordAge <= 0 {
		limits.MaxRecordAge = defaultMaxRecordAge
	}
	if limits.HealthTimeout <= 0 {
		limits.HealthTimeout = defaultHealthTimeout
	}
	if limits.RequestTimeout <= 0 {
		limits.RequestTimeout = defaultRequestTimeout
	}
	if limits.MaxAttempts <= 0 {
		limits.MaxAttempts = defaultMaxAttempts
	}
	if limits.RetryBackoff <= 0 {
		limits.RetryBackoff = defaultRetryBackoff
	}
	if limits.Cooldown <= 0 {
		limits.Cooldown = defaultCooldown
	}
	if limits.LockWait < 0 {
		limits.LockWait = 0
	} else if limits.LockWait == 0 {
		limits.LockWait = defaultLockWait
	}
	if limits.MaxBatches <= 0 {
		limits.MaxBatches = defaultMaxBatches
	}
	if limits.FlushTimeout <= 0 {
		limits.FlushTimeout = defaultFlushTimeout
	}
	// One record must always fit in a request and in the local queue.
	if limits.MaxRequestBytes < limits.MaxRecordBytes+1024 {
		limits.MaxRequestBytes = limits.MaxRecordBytes + 1024
	}
	if limits.MaxQueuedBytes < int64(limits.MaxRecordBytes) {
		limits.MaxQueuedBytes = int64(limits.MaxRecordBytes)
	}
	if limits.MaxReportBodyBytes > limits.MaxRecordBytes {
		limits.MaxReportBodyBytes = limits.MaxRecordBytes
	}
	return limits
}

func defaultDirectory() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("telemetry: locate state directory: LOCALAPPDATA is not set")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("telemetry: locate state directory: %w", err)
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if !filepath.IsAbs(base) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("telemetry: locate state directory: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(base, "lucy", stateDirectoryName), nil
}

func normalizeEndpoint(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("telemetry: invalid endpoint: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", errors.New("telemetry: endpoint must use https")
	}
	if parsed.Host == "" {
		return "", errors.New("telemetry: endpoint has no host")
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return "", errors.New("telemetry: endpoint must use https outside loopback")
	}
	if parsed.User != nil {
		return "", errors.New("telemetry: endpoint must not contain credentials")
	}
	if parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", errors.New("telemetry: endpoint must not contain a query or fragment")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func channelCollectsByDefault(channel string) bool {
	return channel == "alpha" || channel == "beta"
}

func environmentDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvironmentVariable))) {
	case "":
		return false
	case "0", "off", "false", "no", "disabled", "opt-out", "opt_out":
		return true
	case "1", "on", "true", "yes", "enabled", "opt-in", "opt_in":
		return false
	default:
		// An unrecognized explicit value is treated as a request to stop.
		return true
	}
}

func sanitizeVersion(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if len(raw) > 128 {
		raw = raw[:128]
	}
	var builder strings.Builder
	for _, character := range raw {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case strings.ContainsRune("._+-", character):
		default:
			return sanitizeToken(raw, 64)
		}
		builder.WriteRune(character)
		if builder.Len() >= 64 {
			break
		}
	}
	return builder.String()
}

func sanitizeCommit(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if len(raw) < 7 || len(raw) > 40 {
		return ""
	}
	for _, character := range raw {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return ""
		}
	}
	return raw
}
