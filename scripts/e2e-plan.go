//go:build ignore

// Command e2e-plan derives the reusable e2e workflow matrix from the changed
// domains and the repository's compile fixtures, so the matrix and a local
// `go test ./e2e/compile` agree on which fixtures exist.
package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

type suite struct {
	Domain         string `json:"domain,omitempty"`
	Group          string `json:"group"`
	Suite          string `json:"suite"`
	Command        string `json:"command"`
	Timeout        int    `json:"timeout"`
	Sandboxes      string `json:"sandboxes"`
	Java           string `json:"java"`
	Gradle         bool   `json:"gradle"`
	CacheKey       string `json:"cache-key"`
	CachePattern   string `json:"cache-pattern"`
	ReportArtifact string `json:"report-artifact"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e-plan: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	domainsRaw := os.Getenv("DOMAINS")
	if domainsRaw == "" {
		return errors.New("DOMAINS is not set")
	}
	output := os.Getenv("GITHUB_OUTPUT")
	if output == "" {
		return errors.New("GITHUB_OUTPUT is not set")
	}
	var domains []string
	if err := json.Unmarshal([]byte(domainsRaw), &domains); err != nil {
		return fmt.Errorf("parse DOMAINS: %w", err)
	}

	full := os.Getenv("COMPILE_FULL") == "true"
	var fixtures []string
	if full {
		var err error
		fixtures, err = compileFixtures()
		if err != nil {
			return err
		}
		if len(fixtures) == 0 {
			return errors.New("no compile fixtures found")
		}
	}

	matrix := make([]suite, 0, len(allSuites)+len(fixtures))
	for _, candidate := range allSuites {
		if slices.Contains(domains, candidate.Domain) {
			candidate.Domain = ""
			matrix = append(matrix, candidate)
		}
	}

	wantCompile := full || slices.Contains(domains, "compile")
	if wantCompile && full {
		for _, fixture := range fixtures {
			matrix = append(matrix, suite{
				Group:   "compile",
				Suite:   fixture,
				Command: fmt.Sprintf(`go test -tags e2e -count=1 -timeout 40m ./e2e/compile -run "^TestCompile/%s$"`, fixture),
				Timeout: 45,
				Gradle:  true,
			})
		}
	} else if wantCompile {
		matrix = append(matrix, suite{
			Group:        "compile",
			Suite:        "smoke",
			Command:      "LUCY_COMPILE_LAYER=smoke go test -tags e2e -count=1 -timeout 55m -parallel 3 ./e2e/compile",
			Timeout:      60,
			Gradle:       true,
			CacheKey:     "smoke",
			CachePattern: "e2e/testdata/repo/*.yaml",
		})
	}

	data, err := json.Marshal(matrix)
	if err != nil {
		return fmt.Errorf("encode matrix: %w", err)
	}
	file, err := os.OpenFile(output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open GITHUB_OUTPUT: %w", err)
	}
	defer file.Close()
	if _, err := fmt.Fprintf(file, "matrix=%s\n", data); err != nil {
		return fmt.Errorf("write GITHUB_OUTPUT: %w", err)
	}
	return nil
}

func compileFixtures() ([]string, error) {
	entries, err := os.ReadDir("e2e/testdata/repo")
	if err != nil {
		return nil, fmt.Errorf("read compile fixtures: %w", err)
	}
	fixtures := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		fixtures = append(fixtures, strings.TrimSuffix(entry.Name(), ".yaml"))
	}
	slices.Sort(fixtures)
	return fixtures, nil
}

var allSuites = []suite{
	{
		Domain:         "heuristic",
		Group:          "heuristic",
		Suite:          "smoke",
		Command:        "LUCY_HEURISTIC_LAYER=smoke go test -tags e2e -count=1 -timeout 40m -parallel 3 -v ./e2e/heuristic",
		Timeout:        45,
		ReportArtifact: "lucy-heuristic-smoke",
	},
	{
		Domain:    "init",
		Group:     "init",
		Suite:     "init",
		Command:   "go test -tags e2e -count=1 -timeout 25m -v ./e2e/init -run 'TestInitialize/(cobblemon-1211|create-plus-1211|quilt-modpack-1201)'",
		Timeout:   30,
		Sandboxes: "cobblemon-1211,create-plus-1211,quilt-modpack-1201",
	},
	{
		Domain:  "init",
		Group:   "install",
		Suite:   "quilt-lifecycle",
		Command: "go test -tags e2e -count=1 -timeout 25m -v ./e2e/install -run '^TestQuiltLifecycle$'",
		Timeout: 30,
		Java:    "17",
	},
}
