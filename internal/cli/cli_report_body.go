package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxReportMessageBytes = 512
	maxReportStages       = 32
	maxReportStack        = 32
	maxReportCommandBytes = 256
)

// StageCount is one captured diagnostic stage and how often it repeated.
// Stages repeat because a command retries work, such as one resolution per
// candidate combination, so the count carries the retry width that the plain
// message loses.
type StageCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// ReportBody is the machine-readable body of a submitted failure report. It
// carries the failure code and the projected facts of the error tree, the same
// evidence the error document shows in machine mode, without the human title,
// detail lines, hint, or log path.
type ReportBody struct {
	Command string         `json:"command"`
	Code    string         `json:"code"`
	Message string         `json:"message,omitempty"`
	Stages  []StageCount   `json:"stages,omitempty"`
	Facts   map[string]any `json:"facts,omitempty"`
	Stack   []string       `json:"stack,omitempty"`
}

// Encode renders the body as one line of JSON bounded for transport. The
// telemetry client sanitizes it afterwards, so this method only enforces the
// size limits that keep a report small.
func (b ReportBody) Encode() string {
	b.Command = truncateFactText(b.Command, maxReportCommandBytes)
	b.Code = truncateFactText(b.Code, 64)
	b.Message = truncateFactText(collapseRepeatedLines(b.Message), maxReportMessageBytes)
	if len(b.Stages) > maxReportStages {
		b.Stages = b.Stages[:maxReportStages]
	}
	if len(b.Stack) > maxReportStack {
		b.Stack = b.Stack[:maxReportStack]
	}
	payload, err := json.Marshal(b)
	if err != nil {
		return fmt.Sprintf("command=%s code=%s", b.Command, b.Code)
	}
	return string(payload)
}

// collapseRepeatedLines drops consecutive duplicate lines so a joined error
// that repeated the same cause five times reads as one cause.
func collapseRepeatedLines(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	collapsed := lines[:0]
	for _, line := range lines {
		if len(collapsed) > 0 && collapsed[len(collapsed)-1] == line {
			continue
		}
		collapsed = append(collapsed, line)
	}
	return strings.Join(collapsed, "\n")
}

// StageCounts groups captured stage messages by identity, keeping the order in
// which each first appeared.
func StageCounts(stages []string) []StageCount {
	if len(stages) == 0 {
		return nil
	}
	counts := make([]StageCount, 0, len(stages))
	indexes := make(map[string]int, len(stages))
	for _, stage := range stages {
		if position, found := indexes[stage]; found {
			counts[position].Count++
			continue
		}
		indexes[stage] = len(counts)
		counts = append(counts, StageCount{Code: stage, Count: 1})
	}
	return counts
}
