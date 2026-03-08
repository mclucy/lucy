package cmd

import (
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
)

var (
	aggregateCandidatesOnce sync.Once
	aggregateCandidates     []CompletionCandidate
)

// AggregatePackageCandidates returns de-duplicated offline package name candidates
// by combining: static platforms, cache-derived names, and locally installed packages.
// Always returns safely - never panics, never makes network calls.
func AggregatePackageCandidates() []CompletionCandidate {
	aggregateCandidatesOnce.Do(func() {
		defer func() {
			if recover() != nil {
				aggregateCandidates = []CompletionCandidate{}
			}
		}()

		combined := make([]CompletionCandidate, 0)
		combined = append(combined, StaticPlatformCandidates()...)
		combined = append(combined, CacheDerivedCandidates()...)
		combined = append(combined, LocalInstalledCandidates()...)
		aggregateCandidates = DeduplicateCandidates(combined)
	})

	if len(aggregateCandidates) == 0 {
		return []CompletionCandidate{}
	}

	out := make([]CompletionCandidate, len(aggregateCandidates))
	copy(out, aggregateCandidates)
	return out
}

// CacheDerivedCandidates extracts package name hints from the download cache.
// Uses heuristic approach: strips version/extension from cache entry filenames.
// Returns empty slice on any error - fail-safe design.
func CacheDerivedCandidates() (candidates []CompletionCandidate) {
	defer func() {
		if recover() != nil {
			candidates = []CompletionCandidate{}
		}
	}()

	entries := cache.Network().All()
	if len(entries) == 0 {
		return []CompletionCandidate{}
	}

	candidates = make([]CompletionCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.Filename == "" {
			continue
		}

		name := cacheFilenameToNameHint(entry.Filename)
		if name == "" {
			continue
		}

		candidates = append(candidates, CompletionCandidate{
			Value:       name,
			Description: "Cache-derived package hint",
		})
	}

	return candidates
}

// LocalInstalledCandidates returns packages installed in current server directory (via probe).
// Returns empty slice if probe fails or no packages installed.
// IMPORTANT: Only call in completion path if probe is lightweight enough.
func LocalInstalledCandidates() (candidates []CompletionCandidate) {
	defer func() {
		if recover() != nil {
			candidates = []CompletionCandidate{}
		}
	}()

	info := probe.ServerInfo()
	if len(info.Packages) == 0 {
		return []CompletionCandidate{}
	}

	candidates = make([]CompletionCandidate, 0, len(info.Packages))
	for _, pkg := range info.Packages {
		if pkg.Id.Name == "" {
			continue
		}

		value := string(pkg.Id.Name)
		if pkg.Id.Platform != types.PlatformAny {
			value = pkg.Id.StringPlatformName()
		}

		candidates = append(candidates, CompletionCandidate{
			Value:       value,
			Description: "Installed package",
		})
	}

	return candidates
}

// DeduplicateCandidates removes exact Value duplicates from a candidate list (preserves order).
func DeduplicateCandidates(candidates []CompletionCandidate) []CompletionCandidate {
	if len(candidates) == 0 {
		return []CompletionCandidate{}
	}

	seen := make(map[string]struct{}, len(candidates))
	result := make([]CompletionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Value == "" {
			continue
		}
		if _, ok := seen[candidate.Value]; ok {
			continue
		}
		seen[candidate.Value] = struct{}{}
		result = append(result, candidate)
	}

	return result
}

func cacheFilenameToNameHint(filename string) string {
	base := filepath.Base(strings.TrimSpace(filename))
	if base == "" || base == "." {
		return ""
	}

	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name == "" {
		return ""
	}

	parts := strings.Split(name, "-")
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0])
	}

	nameParts := make([]string, 0, len(parts))
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if looksLikeVersionSegment(part) {
			if i == 0 {
				return ""
			}
			break
		}
		nameParts = append(nameParts, part)
	}

	if len(nameParts) == 0 {
		return ""
	}

	return strings.Join(nameParts, "-")
}

func looksLikeVersionSegment(segment string) bool {
	if segment == "" {
		return false
	}

	trimmed := strings.TrimPrefix(strings.TrimPrefix(segment, "v"), "V")
	if trimmed == "" {
		return false
	}

	hasDigit := false
	for _, r := range trimmed {
		if unicode.IsDigit(r) {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return false
	}

	first := []rune(trimmed)[0]
	return unicode.IsDigit(first) || strings.Contains(trimmed, ".")
}
