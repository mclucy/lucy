//go:build integration

package cache

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	cacheChildEnvironment = "LUCY_CACHE_INTEGRATION_PROCESS"
	cacheChildServer      = "LUCY_CACHE_INTEGRATION_SERVER"
	cacheChildJob         = "LUCY_CACHE_INTEGRATION_JOB"
	cacheChildCount       = "LUCY_CACHE_INTEGRATION_COUNT"

	cacheChildReportPrefix = "cache-child-report "
)

// cacheChildReport is the helper process side of the integration protocol.
// A helper prints one JSON line so a parent never has to guess its outcome.
type cacheChildReport struct {
	Scenario   string   `json:"scenario"`
	Keys       []string `json:"keys,omitempty"`
	Misses     int      `json:"misses"`
	Hits       int      `json:"hits"`
	CacheHit   bool     `json:"cache_hit"`
	CacheOn    bool     `json:"cache_on"`
	CacheDir   string   `json:"cache_dir"`
	TempUsable bool     `json:"temp_usable"`
	DataSHA256 string   `json:"data_sha256,omitempty"`
	DataLength int      `json:"data_length,omitempty"`
}

// cacheChild re-executes the integration test binary as a helper process.
type cacheChild struct {
	command *exec.Cmd
	output  bytes.Buffer
}

func startCacheChild(t *testing.T, test string, env map[string]string) *cacheChild {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	command := exec.CommandContext(
		ctx,
		os.Args[0],
		"-test.run=^"+test+"$",
		"-test.timeout=55s",
	)
	command.Env = append(os.Environ(), cacheChildEnvironment+"=1", "GORACE=atexit_sleep_ms=0")
	for key, value := range env {
		command.Env = append(command.Env, key+"="+value)
	}
	child := &cacheChild{command: command}
	command.Stdout = &child.output
	command.Stderr = &child.output
	t.Cleanup(cancel)
	if err := command.Start(); err != nil {
		t.Fatalf("start helper process for %s: %v", test, err)
	}
	return child
}

func (c *cacheChild) wait(t *testing.T) cacheChildReport {
	t.Helper()
	waitErr := c.command.Wait()
	report, parseErr := parseCacheChildReport(&c.output)
	if parseErr != nil {
		t.Fatalf(
			"helper process output:\n%s\nparse report: %v",
			c.output.String(),
			parseErr,
		)
	}
	if waitErr != nil {
		t.Fatalf("helper process failed: %v\n%s", waitErr, c.output.String())
	}
	return report
}

func runCacheChild(t *testing.T, test string, env map[string]string) cacheChildReport {
	t.Helper()
	child := startCacheChild(t, test, env)
	return child.wait(t)
}

func parseCacheChildReport(output io.Reader) (cacheChildReport, error) {
	var report cacheChildReport
	found := false
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, cacheChildReportPrefix) {
			continue
		}
		payload := strings.TrimPrefix(line, cacheChildReportPrefix)
		if err := json.Unmarshal([]byte(payload), &report); err != nil {
			return report, fmt.Errorf("decode %q: %w", payload, err)
		}
		found = true
	}
	if err := scanner.Err(); err != nil {
		return report, err
	}
	if !found {
		return report, errors.New("helper process reported nothing")
	}
	return report, nil
}

func reportCacheChild(t *testing.T, report cacheChildReport) {
	t.Helper()
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("encode helper report: %v", err)
	}
	fmt.Println(cacheChildReportPrefix + string(encoded))
}

// cacheChildScenario reports the helper scenario this process was started for.
func cacheChildScenario() (string, bool) {
	if os.Getenv(cacheChildEnvironment) == "" {
		return "", false
	}
	scenario := os.Getenv(cacheChildJob)
	return scenario, scenario != ""
}

// cacheChildDestination returns a destination directory that never depends on
// the shared TMPDIR of the parent process.
func cacheChildDestination(t *testing.T, name string) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "lucy-cache-child-"+name+"-")
	if err != nil {
		t.Fatalf("create destination directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func artifactNameFromURL(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	return path.Base(parsed.Path)
}

// TestCacheProcessWriter is the helper half of
// TestCacheProcessWritesAreVisibleAcrossProcesses.
func TestCacheProcessWriter(t *testing.T) {
	scenario, isChild := cacheChildScenario()
	if !isChild || !strings.HasPrefix(scenario, "worker-") {
		t.Skip("helper process started by the cross-process cache test")
	}
	server := os.Getenv(cacheChildServer)
	if server == "" {
		t.Fatalf("%s is not set", cacheChildServer)
	}
	count, err := strconv.Atoi(os.Getenv(cacheChildCount))
	if err != nil || count <= 0 {
		t.Fatalf("%s=%q is not a positive count", cacheChildCount, os.Getenv(cacheChildCount))
	}
	report := cacheChildReport{
		Scenario: scenario,
		Keys:     make([]string, 0, count),
	}
	for i := range count {
		name := scenario + "-" + strconv.Itoa(i) + ".jar"
		download := server + artifactRoute + name
		directory := cacheChildDestination(t, name)
		result, err := CachedDownload(
			download,
			directory,
			DownloadOptions{Kind: KindArtifact},
		)
		if err != nil {
			t.Fatalf("download %s: %v", download, err)
		}
		_ = result.File.Close()
		requireFileBytes(
			t,
			filepath.Join(directory, "indexed-"+name),
			[]byte(fixturePayload(name)),
		)
		report.Keys = append(report.Keys, download)
		if result.CacheHit {
			report.Hits++
		} else {
			report.Misses++
		}
	}
	reportCacheChild(t, report)
}

func TestCacheProcessWritesAreVisibleAcrossProcesses(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("HOME", shared)
	fixture := newCacheFixture(t)
	h := installNetworkCacheAt(t, shared, DefaultCacheConfig())
	const workers = 6
	const perWorker = 3

	expected := make([]string, 0, workers*perWorker)
	for worker := range workers {
		scenario := "worker-" + strconv.Itoa(worker)
		for i := range perWorker {
			name := scenario + "-" + strconv.Itoa(i) + ".jar"
			expected = append(expected, fixture.artifactURL(name))
		}
	}

	children := make([]*cacheChild, 0, workers)
	for worker := range workers {
		children = append(
			children,
			startCacheChild(
				t,
				"TestCacheProcessWriter",
				map[string]string{
					"HOME":           shared,
					"XDG_CACHE_HOME": filepath.Join(shared, ".cache"),
					"LOCALAPPDATA":   filepath.Join(shared, ".cache"),
					cacheChildServer: fixture.server.URL,
					cacheChildJob:    "worker-" + strconv.Itoa(worker),
					cacheChildCount:  strconv.Itoa(perWorker),
				},
			),
		)
	}
	written := make([]string, 0, workers*perWorker)
	for _, child := range children {
		report := child.wait(t)
		if report.Misses != perWorker || report.Hits != 0 {
			t.Fatalf(
				"helper %s reported %d misses and %d hits, want %d misses",
				report.Scenario,
				report.Misses,
				report.Hits,
				perWorker,
			)
		}
		if len(report.Keys) != perWorker {
			t.Fatalf(
				"helper %s reported %d keys, want %d",
				report.Scenario,
				len(report.Keys),
				perWorker,
			)
		}
		written = append(written, report.Keys...)
	}
	if len(written) != len(expected) {
		t.Fatalf("helpers wrote %d keys, want %d", len(written), len(expected))
	}

	entries := h.All()
	if len(entries) != len(expected) {
		t.Fatalf(
			"index holds %d entries, want %d from %d processes",
			len(entries),
			len(expected),
			workers,
		)
	}
	if objects := cacheObjects(t, h); len(objects) != len(expected) {
		t.Fatalf(
			"cache store holds %d objects, want %d",
			len(objects),
			len(expected),
		)
	}
	for _, key := range expected {
		hit, data, err := h.GetBytes(key)
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		if !hit {
			t.Fatalf("entry %s written by another process is missing", key)
		}
		name := artifactNameFromURL(t, key)
		if want := []byte(fixturePayload(name)); !bytes.Equal(data, want) {
			t.Fatalf("entry %s holds %q, want %q", key, data, want)
		}
	}
	if fixture.downloads() != int64(len(expected)) {
		t.Fatalf(
			"server served %d downloads, want %d",
			fixture.downloads(),
			len(expected),
		)
	}
}
