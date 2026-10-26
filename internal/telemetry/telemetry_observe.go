package telemetry

import (
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/mclucy/lucy/internal/agent"
)

type callerField struct {
	Kind    string  `json:"kind"`
	Harness *string `json:"harness"`
}

type serverObservation struct {
	mu        sync.Mutex
	product   string
	minecraft string
}

var observedServer serverObservation

// ObserveServer records the server identity the current operation actually
// used. Callers report values they already resolved; telemetry never scans for
// them.
func ObserveServer(product, minecraft string) {
	observedServer.mu.Lock()
	defer observedServer.mu.Unlock()
	if value := cleanIdentity(product); value != "" {
		observedServer.product = value
	}
	if value := cleanIdentity(minecraft); value != "" {
		observedServer.minecraft = value
	}
}

func serverIdentity() (string, string) {
	observedServer.mu.Lock()
	defer observedServer.mu.Unlock()
	return observedServer.product, observedServer.minecraft
}

func cleanIdentity(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "none", "unknown", "any", "stable", "beta", "latest", "release", "snapshot":
		return ""
	default:
		return value
	}
}

func caller(harness string) callerField {
	detected := agent.Harness()
	if harness == "" {
		harness = detected
	}
	kind := "unknown"
	switch {
	case detected != "" || harness != "":
		kind = "agent"
	case stdinIsTerminal():
		kind = "human"
	}
	return callerField{Kind: kind, Harness: optional(harness)}
}

func optional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func osName() string {
	return runtime.GOOS
}

func architecture() string {
	return runtime.GOARCH
}
