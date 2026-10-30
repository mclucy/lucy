package telemetry

import (
	"encoding/json/jsontext"
	"fmt"
	"strings"
	"time"
)

const (
	maxContexts          = 16
	maxContextDataBytes  = 8 << 10
	maxContextTypeLength = 64
	maxEventDuration     = 30 * 24 * time.Hour
)

type eventPayload struct {
	InvocationID     string           `json:"invocation_id"`
	InstallationID   string           `json:"installation_id"`
	OccurredAt       string           `json:"occurred_at"`
	Command          string           `json:"command"`
	LucyVersion      string           `json:"lucy_version"`
	LucyCommit       string           `json:"lucy_commit"`
	Channel          string           `json:"channel"`
	OS               string           `json:"os"`
	Arch             string           `json:"arch"`
	DurationMS       int64            `json:"duration_ms"`
	Outcome          Outcome          `json:"outcome"`
	ErrorCode        string           `json:"error_code"`
	SchemaVersion    int              `json:"schema_version"`
	CollectionPolicy string           `json:"collection_policy"`
	Contexts         []contextPayload `json:"contexts,omitempty"`
}

type contextPayload struct {
	Type   string         `json:"type"`
	Status ContextStatus  `json:"status"`
	Data   jsontext.Value `json:"data,omitzero"`
}

type reportPayload struct {
	ReportID         string `json:"report_id"`
	InstallationID   string `json:"installation_id"`
	InvocationID     string `json:"invocation_id,omitempty"`
	Kind             string `json:"kind"`
	Agent            string `json:"agent,omitempty"`
	Model            string `json:"model,omitempty"`
	CreatedAt        string `json:"created_at"`
	SchemaVersion    int    `json:"schema_version"`
	CollectionPolicy string `json:"collection_policy"`
	Body             string `json:"body"`
}

type preparedEvent struct {
	invocationID string
	command      string
	durationMS   int64
	outcome      Outcome
	errorCode    string
	occurredAt   time.Time
	contexts     []contextPayload
}

type preparedReport struct {
	kind         ReportKind
	invocationID string
	agent        string
	model        string
	body         string
	createdAt    time.Time
}

func (c *Client) prepareEvent(event Event, now time.Time) (preparedEvent, error) {
	invocationID, err := normalizeUUID(event.InvocationID)
	if err != nil {
		return preparedEvent{}, fmt.Errorf("%w: invocation id: %v", ErrInvalidEvent, err)
	}
	command, err := normalizeCommand(event.Command)
	if err != nil {
		return preparedEvent{}, fmt.Errorf("%w: command must be a command path without arguments", ErrInvalidEvent)
	}
	if event.Duration < 0 || event.Duration > maxEventDuration {
		return preparedEvent{}, fmt.Errorf("%w: duration out of range", ErrInvalidEvent)
	}
	switch event.Outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomeCanceled:
	default:
		return preparedEvent{}, fmt.Errorf("%w: unknown outcome %q", ErrInvalidEvent, event.Outcome)
	}
	if event.ErrorCode != "" && !validCode(event.ErrorCode, 64) {
		return preparedEvent{}, fmt.Errorf("%w: invalid error code", ErrInvalidEvent)
	}
	contexts, err := normalizeContexts(event.Contexts)
	if err != nil {
		return preparedEvent{}, err
	}
	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = now
	}
	return preparedEvent{
		invocationID: invocationID,
		command:      command,
		durationMS:   event.Duration.Milliseconds(),
		outcome:      event.Outcome,
		errorCode:    event.ErrorCode,
		occurredAt:   occurredAt.UTC(),
		contexts:     contexts,
	}, nil
}

func normalizeContexts(contexts []Context) ([]contextPayload, error) {
	if len(contexts) == 0 {
		return nil, nil
	}
	if len(contexts) > maxContexts {
		return nil, fmt.Errorf("%w: too many contexts", ErrInvalidEvent)
	}
	normalized := make([]contextPayload, 0, len(contexts))
	for _, context := range contexts {
		kind := strings.ToLower(strings.TrimSpace(string(context.Kind)))
		if !validContextType(kind) {
			return nil, fmt.Errorf("%w: invalid context type", ErrInvalidEvent)
		}
		switch context.Status {
		case ContextPresent, ContextAbsent, ContextFailed:
		default:
			return nil, fmt.Errorf("%w: invalid context status", ErrInvalidEvent)
		}
		payload := contextPayload{Type: kind, Status: context.Status}
		if context.Status == ContextAbsent {
			if context.Data != nil {
				return nil, fmt.Errorf("%w: absent context must not carry data", ErrInvalidEvent)
			}
			normalized = append(normalized, payload)
			continue
		}
		if context.Data == nil {
			if context.Status == ContextPresent {
				return nil, fmt.Errorf("%w: present context requires data", ErrInvalidEvent)
			}
			normalized = append(normalized, payload)
			continue
		}
		encoded, err := marshalCanonical(context.Data)
		if err != nil {
			return nil, fmt.Errorf("%w: context data is not JSON: %v", ErrInvalidEvent, err)
		}
		if len(encoded) == 0 || string(encoded) == "null" {
			if context.Status == ContextPresent {
				return nil, fmt.Errorf("%w: present context requires data", ErrInvalidEvent)
			}
			normalized = append(normalized, payload)
			continue
		}
		if len(encoded) > maxContextDataBytes {
			return nil, fmt.Errorf("%w: context data exceeds %d bytes", ErrInvalidEvent, maxContextDataBytes)
		}
		payload.Data = jsontext.Value(encoded)
		normalized = append(normalized, payload)
	}
	return normalized, nil
}

func validContextType(value string) bool {
	if value == "" || len(value) > maxContextTypeLength {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		valid := character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9'
		if index > 0 {
			valid = valid || character == '-' || character == '_' || character == '.'
		}
		if !valid {
			return false
		}
	}
	return true
}

func (c *Client) buildEventPayload(event preparedEvent, state *persistedState) eventPayload {
	return eventPayload{
		InvocationID:     event.invocationID,
		InstallationID:   state.InstallationID,
		OccurredAt:       event.occurredAt.Format(time.RFC3339),
		Command:          event.command,
		LucyVersion:      c.version,
		LucyCommit:       c.commit,
		Channel:          c.channel,
		OS:               c.os,
		Arch:             c.arch,
		DurationMS:       event.durationMS,
		Outcome:          event.outcome,
		ErrorCode:        event.errorCode,
		SchemaVersion:    SchemaVersion,
		CollectionPolicy: payloadPolicy(state.Policy),
		Contexts:         event.contexts,
	}
}
