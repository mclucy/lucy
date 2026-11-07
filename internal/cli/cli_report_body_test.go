package cli

import (
	"encoding/json/v2"
	"testing"
)

func TestReportBodyRetainsStructuredInvalidUTF8Diagnostics(t *testing.T) {
	t.Parallel()
	encoded := (ReportBody{
		Command: "install",
		Code:    "install.failed",
		Message: "installer \xff failed",
		Facts:   map[string]any{"detail": "stderr \xfe"},
	}).Encode()
	var body struct {
		Command string            `json:"command"`
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Facts   map[string]string `json:"facts"`
	}
	if err := json.Unmarshal([]byte(encoded), &body); err != nil {
		t.Fatalf("report lost its JSON transport: %v; body: %q", err, encoded)
	}
	if body.Command != "install" || body.Code != "install.failed" {
		t.Fatalf("report lost command or failure identity: %+v", body)
	}
	if body.Message != "installer \ufffd failed" || body.Facts["detail"] != "stderr \ufffd" {
		t.Fatalf("report lost subprocess diagnostic text: %+v", body)
	}
}
