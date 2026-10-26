package telemetry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/report"
)

const (
	// OutcomeSuccess, OutcomeFailure, and OutcomeCancelled are the only event
	// outcomes.
	OutcomeSuccess   = "success"
	OutcomeFailure   = "failure"
	OutcomeCancelled = "cancelled"

	eventPath  = "/events"
	reportPath = "/agent-reports"

	handoffLimit   = 4 << 20
	handoffMaxAge  = 15 * time.Minute
	uploadDeadline = 10 * time.Second
)

// endpoint is injected by the build. An unreachable or empty receiver disables
// uploads without affecting commands.
var endpoint string

var buildIdentity struct {
	mu      sync.RWMutex
	version string
	commit  string
}

// SetBuild records the identity the binary already reports. Telemetry never
// invents a version or commit.
func SetBuild(version, commit string) {
	buildIdentity.mu.Lock()
	defer buildIdentity.mu.Unlock()
	buildIdentity.version = version
	buildIdentity.commit = commit
}

func buildFields() (string, *string) {
	buildIdentity.mu.RLock()
	defer buildIdentity.mu.RUnlock()
	version := buildIdentity.version
	if version == "" {
		version = "unknown"
	}
	return version, optional(buildIdentity.commit)
}

type environmentFields struct {
	ParticipantID    string      `json:"participant_id"`
	LucyVersion      string      `json:"lucy_version"`
	LucyCommit       *string     `json:"lucy_commit"`
	OSName           string      `json:"os_name"`
	OSVersion        *string     `json:"os_version"`
	Architecture     string      `json:"architecture"`
	ServerType       *string     `json:"server_type"`
	MinecraftVersion *string     `json:"minecraft_version"`
	Caller           callerField `json:"caller"`
}

type event struct {
	environmentFields
	Command      string  `json:"command"`
	Outcome      string  `json:"outcome"`
	Error        *string `json:"error"`
	Log          *string `json:"log"`
	LogTruncated bool    `json:"log_truncated"`
}

type agentReport struct {
	environmentFields
	Body    string  `json:"body"`
	Model   *string `json:"model"`
	Harness *string `json:"harness"`
}

type handoff struct {
	Kind       string       `json:"kind"`
	Generation string       `json:"generation"`
	Event      *event       `json:"event,omitempty"`
	Report     *agentReport `json:"report,omitempty"`
	Created    time.Time    `json:"created"`
}

// FinishCommand prepares at most one event for one business command. Every
// failure here leaves the command's own result untouched.
func FinishCommand(command, outcome string, failure error, machine bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Debug("telemetry event failed", "error", recovered)
		}
	}()
	if command == "" {
		return
	}
	cleanupHandoffs()
	value, err := loadState()
	if err != nil {
		log.Debug("telemetry state unavailable", "error", err)
		return
	}
	if !value.Enabled || value.Token == "" {
		return
	}
	value, err = ensureUploadIdentity(value)
	if err != nil {
		log.Debug("telemetry identity unavailable", "error", err)
		return
	}
	if !value.uploadable() {
		return
	}
	if _, err := receiverURL(eventPath); err != nil {
		log.Debug("telemetry receiver unavailable", "error", err)
		return
	}
	record, err := newEvent(value, command, outcome, failure)
	if err != nil {
		log.Debug("telemetry event unavailable", "error", err)
		return
	}
	path, err := writeHandoff(handoff{
		Kind:       "event",
		Generation: value.Generation,
		Event:      record,
		Created:    time.Now().UTC(),
	})
	if err != nil {
		log.Debug("telemetry handoff unavailable", "error", err)
		return
	}
	if err := spawnUploader(path); err != nil {
		_ = os.Remove(path)
		log.Debug("telemetry upload process unavailable", "error", err)
		return
	}
	uploadNotice(machine)
}

func newEvent(value state, command, outcome string, failure error) (*event, error) {
	version, commit := buildFields()
	product, minecraft := serverIdentity()
	record := &event{
		environmentFields: environmentFields{
			ParticipantID:    value.Participant,
			LucyVersion:      version,
			LucyCommit:       commit,
			OSName:           osName(),
			OSVersion:        optional(osVersion()),
			Architecture:     architecture(),
			ServerType:       optional(product),
			MinecraftVersion: optional(minecraft),
			Caller:           caller(""),
		},
		Command:      command,
		Outcome:      outcome,
		LogTruncated: false,
	}
	if outcome == OutcomeFailure && failure != nil {
		filter := newSanitizer(value.Token)
		if message := filter.sanitizeError(failure.Error()); message != "" {
			record.Error = &message
		}
		captured, capturedTruncated := log.Snapshot(handoffLimit)
		if len(captured) > 0 {
			filtered, filteredTruncated := filter.sanitizeLog(string(captured))
			if filtered != "" {
				record.Log = &filtered
			}
			record.LogTruncated = capturedTruncated || filteredTruncated
		}
	}
	return record, nil
}

func writeHandoff(value handoff) (string, error) {
	root, err := outboxRoot()
	if err != nil {
		return "", err
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return "", err
	}
	name, err := randomHex(16)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, name+".json")
	content, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode telemetry handoff: %w", err)
	}
	if err := writePrivateFile(path, content); err != nil {
		return "", err
	}
	return path, nil
}

// cleanupHandoffs removes abandoned handoffs without replaying them.
func cleanupHandoffs() {
	root, err := outboxRoot()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > handoffMaxAge {
			_ = os.Remove(filepath.Join(root, entry.Name()))
		}
	}
}

func uploadNotice(machine bool) {
	if machine {
		log.Info("telemetry upload started")
		return
	}
	report.Info("telemetry is uploading in the background; run `lucy telemetry off` to stop")
}
