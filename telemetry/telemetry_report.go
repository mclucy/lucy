package telemetry

import (
	"fmt"
	"strings"
	"time"
)

func (c *Client) prepareReport(report Report, now time.Time) (preparedReport, bool, error) {
	switch report.Kind {
	case ReportFailure, ReportPanic, ReportAgentFeedback:
	default:
		return preparedReport{}, false, fmt.Errorf("%w: unknown report kind %q", ErrInvalidReport, report.Kind)
	}
	invocationID := ""
	if strings.TrimSpace(report.InvocationID) != "" {
		normalized, err := normalizeUUID(report.InvocationID)
		if err != nil {
			return preparedReport{}, false, fmt.Errorf("%w: invocation id: %v", ErrInvalidReport, err)
		}
		invocationID = normalized
	}
	body, truncated := sanitizeReportText(report.Body, c.limits.MaxReportBodyBytes)
	if body == "" {
		return preparedReport{}, false, fmt.Errorf("%w: body is empty after sanitization", ErrInvalidReport)
	}
	createdAt := report.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	return preparedReport{
		kind:         report.Kind,
		invocationID: invocationID,
		agent:        sanitizeReportIdentifier(report.Agent),
		model:        sanitizeReportIdentifier(report.Model),
		body:         body,
		createdAt:    createdAt.UTC(),
	}, truncated, nil
}

func sanitizeReportIdentifier(raw string) string {
	if len(raw) > 256 {
		raw = raw[:256]
	}
	var builder strings.Builder
	for _, character := range strings.TrimSpace(raw) {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case strings.ContainsRune("._-/:@", character):
		default:
			continue
		}
		builder.WriteRune(character)
		if builder.Len() >= 64 {
			break
		}
	}
	return builder.String()
}
