// Package telemetry collects a bounded record of one command and hands it to a
// detached upload process. It never changes a command's result when it fails.
package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxLogBytes is the retained tail of this process's diagnostics.
	maxLogBytes = 262144
	// maxReportBytes is the accepted report body size.
	maxReportBytes = 262144
	// maxErrorBytes bounds the failure description.
	maxErrorBytes = 8192
	// maxTokenBytes bounds one token read from standard input.
	maxTokenBytes = 512
)

// state is the user-scoped telemetry configuration. Generation invalidates
// handoffs left behind by a previous on/off cycle.
type state struct {
	Enabled     bool   `json:"enabled"`
	Token       string `json:"token,omitempty"`
	Participant string `json:"participant_id,omitempty"`
	Generation  string `json:"generation,omitempty"`
}

func (s state) uploadable() bool {
	return s.Enabled && s.Token != "" && s.Participant != "" && s.Generation != ""
}

func stateRoot() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("telemetry configuration directory: %w", err)
	}
	return filepath.Join(base, "lucy", "telemetry"), nil
}

func statePath() (string, error) {
	root, err := stateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "state.json"), nil
}

func outboxRoot() (string, error) {
	root, err := stateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "outbox"), nil
}

func activeRoot() (string, error) {
	root, err := stateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "active"), nil
}

// loadState reads the user configuration. A missing file reports the build
// default. A present but unreadable or malformed file is an error: telemetry
// stops instead of replacing it with the default.
func loadState() (state, error) {
	path, err := statePath()
	if err != nil {
		return state{}, err
	}
	if err := checkPrivateDirectory(filepath.Dir(path)); err != nil {
		return state{}, err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state{Enabled: defaultEnabled}, nil
	}
	if err != nil {
		return state{}, fmt.Errorf("read telemetry state: %w", err)
	}
	if err := checkPrivateFile(path); err != nil {
		return state{}, err
	}
	var loaded state
	if err := json.Unmarshal(content, &loaded); err != nil {
		return state{}, fmt.Errorf("telemetry state is corrupt: %w", err)
	}
	return loaded, nil
}

func saveState(value state) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	content, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode telemetry state: %w", err)
	}
	content = append(content, '\n')
	if err := writePrivateFile(path, content); err != nil {
		return fmt.Errorf("write telemetry state: %w", err)
	}
	return nil
}

// ensureUploadIdentity creates the participant identifier the first time a
// token makes uploads possible.
func ensureUploadIdentity(value state) (state, error) {
	if !value.Enabled || value.Token == "" {
		return value, nil
	}
	changed := false
	if value.Participant == "" {
		participant, err := randomUUID()
		if err != nil {
			return state{}, err
		}
		value.Participant = participant
		changed = true
	}
	if value.Generation == "" {
		generation, err := randomHex(16)
		if err != nil {
			return state{}, err
		}
		value.Generation = generation
		changed = true
	}
	if changed {
		if err := saveState(value); err != nil {
			return state{}, err
		}
	}
	return value, nil
}

func readTokenInput(reader io.Reader) (string, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maxTokenBytes+1))
	if err != nil {
		return "", fmt.Errorf("read telemetry token: %w", err)
	}
	if len(content) > maxTokenBytes {
		return "", fmt.Errorf("telemetry token exceeds %d bytes", maxTokenBytes)
	}
	token := strings.TrimRight(string(content), "\r\n")
	if token == "" {
		return "", errors.New("telemetry token is empty")
	}
	for _, character := range token {
		if character <= ' ' || character > '~' {
			return "", errors.New("telemetry token must be printable ASCII without spaces")
		}
	}
	return token, nil
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate telemetry identity: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func randomUUID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate telemetry participant: %w", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(buffer[0:4]),
		hex.EncodeToString(buffer[4:6]),
		hex.EncodeToString(buffer[6:8]),
		hex.EncodeToString(buffer[8:10]),
		hex.EncodeToString(buffer[10:16]),
	), nil
}
