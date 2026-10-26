package telemetry

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/report"
)

const maxRequestBytes = 8 << 20

// processStarted anchors the total upload window at process start, not at the
// first network call.
var processStarted = time.Now()

var httpClientFactory = func() *http.Client {
	return &http.Client{
		Timeout: uploadDeadline,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func receiverURL(path string) (string, error) {
	if strings.TrimSpace(endpoint) == "" {
		return "", errors.New("no telemetry receiver is configured in this build")
	}
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", fmt.Errorf("telemetry receiver address: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("telemetry receiver must be an HTTPS address")
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func post(ctx context.Context, address, token string, payload any) error {
	content, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode telemetry request: %w", err)
	}
	if len(content) > maxRequestBytes {
		return fmt.Errorf("telemetry request exceeds %d bytes", maxRequestBytes)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(content))
	if err != nil {
		return fmt.Errorf("prepare telemetry request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := httpClientFactory().Do(request)
	if err != nil {
		return fmt.Errorf("telemetry request: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	if response.StatusCode != http.StatusCreated {
		return fmt.Errorf("telemetry receiver returned %s", response.Status)
	}
	return nil
}

// UploadHandoff consumes one handoff file and attempts one upload. It never
// retries: an unreadable or abandoned file is removed and dropped.
func UploadHandoff(path string) error {
	if err := checkHandoffPath(path); err != nil {
		return err
	}
	deadline := processStarted.Add(uploadDeadline)
	if !time.Now().Before(deadline) {
		return errors.New("telemetry upload window elapsed")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	content, readErr := readHandoff(path)
	if removeErr := os.Remove(path); removeErr != nil {
		return fmt.Errorf("remove telemetry handoff: %w", removeErr)
	}
	if readErr != nil {
		return readErr
	}
	var value handoff
	if err := json.Unmarshal(content, &value); err != nil {
		return fmt.Errorf("decode telemetry handoff: %w", err)
	}
	if value.Kind != "event" || value.Event == nil {
		return errors.New("invalid telemetry handoff")
	}
	stateValue, err := loadState()
	if err != nil {
		return err
	}
	if !handoffAllowed(stateValue, value) {
		return nil
	}
	address, err := receiverURL(eventPath)
	if err != nil {
		return err
	}
	release := registerActive()
	defer release()

	// The switch or the identity may have changed while this process started.
	current, err := loadState()
	if err != nil {
		return err
	}
	if !handoffAllowed(current, value) {
		return nil
	}
	return post(ctx, address, current.Token, value.Event)
}

func handoffAllowed(current state, value handoff) bool {
	if !current.uploadable() || value.Event == nil {
		return false
	}
	return current.Generation == value.Generation &&
		current.Participant == value.Event.ParticipantID
}

func readHandoff(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open telemetry handoff: %w", err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, handoffLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read telemetry handoff: %w", err)
	}
	if len(content) > handoffLimit {
		return nil, fmt.Errorf("telemetry handoff exceeds %d bytes", handoffLimit)
	}
	return content, nil
}

// checkHandoffPath keeps the internal entry from consuming or deleting a file
// outside Lucy's private outbox.
func checkHandoffPath(path string) error {
	root, err := outboxRoot()
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("telemetry handoff path: %w", err)
	}
	if !sameDirectory(filepath.Dir(absolute), filepath.Clean(root)) {
		return errors.New("telemetry handoff is outside the private outbox")
	}
	name := strings.TrimSuffix(filepath.Base(absolute), ".json")
	if len(name) != 32 {
		return errors.New("telemetry handoff name is invalid")
	}
	if _, err := hex.DecodeString(name); err != nil {
		return errors.New("telemetry handoff name is invalid")
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return fmt.Errorf("inspect telemetry handoff: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("telemetry handoff is not a regular file")
	}
	return nil
}

func sameDirectory(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// ReportReady performs the local eligibility check before standard input is
// read, so a disabled or unconfigured command never blocks on a pipe.
func ReportReady() error {
	value, err := loadState()
	if err != nil {
		return err
	}
	if !value.Enabled {
		return errors.New("telemetry is disabled; run `lucy telemetry on --token-stdin` first")
	}
	if value.Token == "" {
		return errors.New("telemetry has no upload token; run `lucy telemetry on --token-stdin` first")
	}
	if _, err := receiverURL(reportPath); err != nil {
		return err
	}
	return nil
}

// SendReport uploads one sanitized report and waits for the receiver to
// confirm persistence. It is not retried.
func SendReport(body, model, harness string, machine bool) error {
	value, err := loadState()
	if err != nil {
		return err
	}
	value, err = ensureUploadIdentity(value)
	if err != nil {
		return err
	}
	if !value.uploadable() {
		return errors.New("telemetry is not configured to upload; run `lucy telemetry on --token-stdin` first")
	}
	address, err := receiverURL(reportPath)
	if err != nil {
		return err
	}
	if !utf8Valid(body) {
		return errors.New("report body is not valid UTF-8")
	}
	if body == "" {
		return errors.New("report body is empty")
	}
	if len(body) > maxReportBytes {
		return fmt.Errorf("report body exceeds %d bytes", maxReportBytes)
	}
	callerValue := caller(harness)
	if harness == "" {
		harness = valueString(callerValue.Harness)
	}
	filtered := newSanitizer(value.Token).sanitize(body)
	if strings.TrimSpace(filtered) == "" {
		return errors.New("report body has no content after privacy filtering")
	}
	if len(filtered) > maxReportBytes {
		return fmt.Errorf("report body exceeds %d bytes after privacy filtering", maxReportBytes)
	}
	version, commit := buildFields()
	product, minecraft := serverIdentity()
	record := agentReport{
		environmentFields: environmentFields{
			ParticipantID:    value.Participant,
			LucyVersion:      version,
			LucyCommit:       commit,
			OSName:           osName(),
			OSVersion:        optional(osVersion()),
			Architecture:     architecture(),
			ServerType:       optional(product),
			MinecraftVersion: optional(minecraft),
			Caller:           callerValue,
		},
		Body:    filtered,
		Model:   optional(model),
		Harness: optional(harness),
	}
	release := registerActive()
	defer release()

	// Recheck immediately before sending so a concurrent `off` wins.
	current, err := loadState()
	if err != nil {
		return err
	}
	if !current.uploadable() ||
		current.Generation != value.Generation ||
		current.Participant != value.Participant {
		return errors.New("telemetry was disabled before the report was sent")
	}
	notifyReportUpload(machine)
	ctx, cancel := context.WithTimeout(context.Background(), uploadDeadline)
	defer cancel()
	if err := post(ctx, address, current.Token, record); err != nil {
		return fmt.Errorf("%w; delivery may be unknown", err)
	}
	return nil
}

// notifyReportUpload announces a synchronous report upload before it starts.
func notifyReportUpload(machine bool) {
	message := "sending telemetry report and waiting for confirmation"
	if machine {
		log.Info(message)
		return
	}
	report.Info(message)
}

func valueString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
