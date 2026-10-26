// Package agent detects coding-agent harnesses from the process environment.
package agent

import (
	"os"
	"strings"
)

// Agent environment detection took reference from Cloudflare's CLI
// https://github.com/cloudflare/cf/blob/main/packages/cli/src/lib/agent-context.ts
type detector struct {
	name   string
	detect func() bool
}

var detectors = []detector{
	{"opencode", func() bool { return exact("OPENCODE", "1") }},
	{"qwen-code", func() bool { return value("QWEN_CODE_SESSION_ID") != "" }},
	{"pi", func() bool {
		return exact("PI_CODING_AGENT", "true") || value("AI_AGENT") == "pi"
	}},
	{"omp", func() bool {
		return exact("OMPCODE", "1") ||
			value("PI_SESSION_FILE") != "" ||
			value("PI_SESSION_ID") != ""
	}},
	{"dsh", func() bool { return exact("DSH_SHELL", "1") }},
	{"cursor", func() bool { return exact("CURSOR_AGENT", "1") }},
	{"claude", func() bool { return exact("CLAUDECODE", "1") }},
	{"codex", func() bool { return value("CODEX_THREAD_ID") != "" }},
	{"amp", func() bool { return value("AMP_CURRENT_THREAD_ID") != "" }},
	{"gemini", func() bool { return exact("GEMINI_CLI", "1") }},
	{"augment", func() bool { return exact("AUGMENT_AGENT", "1") }},
	{"crush", func() bool {
		return exact("CRUSH", "1") || value("AI_AGENT") == "crush"
	}},
	{"github-copilot", func() bool {
		return value("AI_AGENT") == "github_copilot_vscode_agent" ||
			exact("COPILOT_AGENT", "1")
	}},
	{"oz", func() bool { return value("OZ_RUN_ID") != "" }},
}

// Harness reports the detected harness name, or the empty string. The first
// matching detector wins so concurrent signals select the same name every run.
func Harness() string {
	for _, candidate := range detectors {
		if candidate.detect() {
			return candidate.name
		}
	}
	return ""
}

// Detected reports whether any supported agent harness is present.
func Detected() bool {
	return Harness() != ""
}

func value(name string) string {
	raw := strings.TrimSpace(os.Getenv(name))
	switch strings.ToLower(raw) {
	case "", "0", "false", "no", "off":
		return ""
	default:
		return raw
	}
}

func exact(name, expected string) bool {
	return os.Getenv(name) == expected
}
