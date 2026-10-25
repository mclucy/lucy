// Package agent detects coding-agent harnesses from the process environment.
package agent

import (
	"os"
	"strings"
)

// Agent environment detection took reference from Cloudflare's CLI
// https://github.com/cloudflare/cf/blob/main/packages/cli/src/lib/agent-context.ts
var detectors = []func() bool{
	func() bool { return exact("OPENCODE", "1") },
	func() bool { return value("QWEN_CODE_SESSION_ID") != "" },
	func() bool {
		return exact("PI_CODING_AGENT", "true") || value("AI_AGENT") == "pi"
	},
	func() bool { return exact("CURSOR_AGENT", "1") },
	func() bool { return exact("CLAUDECODE", "1") },
	func() bool { return value("CODEX_THREAD_ID") != "" },
	func() bool { return value("AMP_CURRENT_THREAD_ID") != "" },
	func() bool { return exact("GEMINI_CLI", "1") },
	func() bool { return exact("AUGMENT_AGENT", "1") },
	func() bool {
		return exact("CRUSH", "1") || value("AI_AGENT") == "crush"
	},
	func() bool {
		return value("AI_AGENT") == "github_copilot_vscode_agent" ||
			exact("COPILOT_AGENT", "1")
	},
	func() bool { return value("OZ_RUN_ID") != "" },
}

// Detected reports whether any supported agent harness is present.
func Detected() bool {
	for _, detect := range detectors {
		if detect() {
			return true
		}
	}
	return false
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
