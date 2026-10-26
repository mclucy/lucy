package telemetry

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mclucy/lucy/internal/agent"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/report"
)

// EnableResult reports a local configuration change. It never confirms that
// the receiver accepts the token.
type EnableResult struct {
	Enabled bool
}

// Enable turns telemetry on. With readToken it replaces the stored token from
// standard input; without it an existing token is required.
func Enable(readToken bool, reader io.Reader) (EnableResult, error) {
	value, err := loadState()
	if err != nil {
		return EnableResult{}, err
	}
	if readToken {
		token, err := readTokenInput(reader)
		if err != nil {
			return EnableResult{}, err
		}
		value.Token = token
	} else if value.Token == "" {
		return EnableResult{}, errors.New(
			"no telemetry token is stored; run `lucy telemetry on --token-stdin` with the token from your maintainer",
		)
	}
	value.Enabled = true
	value, err = ensureUploadIdentity(value)
	if err != nil {
		return EnableResult{}, err
	}
	if err := saveState(value); err != nil {
		return EnableResult{}, err
	}
	cleanupHandoffs()
	return EnableResult{Enabled: true}, nil
}

// Disable persists the off switch before canceling work, so no new upload can
// start. It keeps the token and drops the participant identity.
func Disable() error {
	value, err := loadState()
	if err != nil {
		return err
	}
	generation, err := randomHex(16)
	if err != nil {
		return err
	}
	value.Enabled = false
	value.Participant = ""
	value.Generation = generation
	if err := saveState(value); err != nil {
		return err
	}
	clearHandoffs()
	cancelActive()
	return nil
}

func clearHandoffs() {
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
		_ = os.Remove(filepath.Join(root, entry.Name()))
	}
}

// NotifyCommand reports the switch state at the start of one business
// command. Machine mode records the notice in diagnostics instead.
func NotifyCommand(machine bool) {
	value, err := loadState()
	if err != nil {
		log.Debug("telemetry state unavailable", "error", err)
		return
	}
	if !value.Enabled {
		return
	}
	message := "telemetry is enabled; run `lucy telemetry off` to disable"
	if value.Token == "" {
		message = "telemetry is enabled but cannot upload yet; run `lucy telemetry on --token-stdin` with your maintainer token"
	}
	if machine {
		log.Info(message)
		return
	}
	report.Info(message)
}

// ReportGuidance returns the agent instruction block when reporting is
// possible. Empty when telemetry is disabled, unconfigured, or no harness is
// detected.
func ReportGuidance() (string, bool) {
	if agent.Harness() == "" {
		return "", false
	}
	value, err := loadState()
	if err != nil || !value.Enabled || value.Token == "" {
		return "", false
	}
	if _, err := receiverURL(reportPath); err != nil {
		return "", false
	}
	return "If a business command fails, finish the diagnosis and submit one report with " +
		"`lucy telemetry agent-report --model <model> --harness <harness> < report.md`. " +
		"The report covers the goal, the failing step, expected and actual results, and only " +
		"the necessary diagnostic excerpt. Do not paste a full session, tool log, or unrelated " +
		"files. Do not report again when the report itself fails, and do not report after every " +
		"successful command. Telemetry uploads automatically.", true
}

func utf8Valid(value string) bool {
	return utf8.ValidString(value)
}

// ReadReportBody reads one bounded Markdown report from standard input.
func ReadReportBody(reader io.Reader) (string, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maxReportBytes+1))
	if err != nil {
		return "", errors.New("read report body: " + err.Error())
	}
	if len(content) > maxReportBytes {
		return "", errors.New("report body exceeds 262144 bytes")
	}
	if !utf8.Valid(content) {
		return "", errors.New("report body is not valid UTF-8")
	}
	if strings.TrimSpace(string(content)) == "" {
		return "", errors.New("report body is empty")
	}
	return string(content), nil
}
