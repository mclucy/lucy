package compile

import (
	"bytes"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type repoFixture struct {
	URL       string `yaml:"url"`
	Commit    string `yaml:"commit"`
	Ecosystem string `yaml:"ecosystem"`
	Project   string `yaml:"project"`
	// Smoke selects the fixture for CI's default compile layer.
	Smoke bool `yaml:"smoke"`
}

type compileFixture struct {
	ID string
	repoFixture
}

// fixtureIDs lists every fixture manifest in repos, in directory order.
func fixtureIDs(t *testing.T, repos string) []string {
	t.Helper()
	entries, err := os.ReadDir(repos)
	if err != nil {
		t.Fatalf("list compile fixtures: %v", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			ids = append(ids, strings.TrimSuffix(entry.Name(), ".yaml"))
		}
	}
	if len(ids) == 0 {
		t.Fatalf("no compile fixtures in %s", repos)
	}
	return ids
}

func selectFixtures(t *testing.T, repos string, smokeOnly bool) []compileFixture {
	t.Helper()
	ids := fixtureIDs(t, repos)
	selected := make([]compileFixture, 0, len(ids))
	for _, id := range ids {
		fixture := readFixture(t, repos, id)
		if smokeOnly && !fixture.Smoke {
			continue
		}
		selected = append(selected, compileFixture{ID: id, repoFixture: fixture})
	}
	return selected
}

func readFixture(t *testing.T, repos, id string) repoFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repos, id+".yaml"))
	if err != nil {
		t.Fatalf("read compile fixture %s: %v", id, err)
	}
	var fixture repoFixture
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatalf("decode compile fixture %s: %v", id, err)
	}
	if fixture.URL == "" {
		t.Fatalf("fixture %s requires a repository URL", id)
	}
	if len(fixture.Commit) != 40 || strings.Trim(fixture.Commit, "0123456789abcdef") != "" {
		t.Fatalf("fixture %s requires a full commit hash, got %q", id, fixture.Commit)
	}
	return fixture
}

func executeCompile(t *testing.T, binary string, fixture compileFixture) {
	t.Helper()
	temporary := t.TempDir()
	checkout := filepath.Join(temporary, "repository")
	if err := os.Mkdir(checkout, 0o755); err != nil {
		t.Fatalf("create checkout: %v", err)
	}
	environment := slices.DeleteFunc(os.Environ(), func(value string) bool {
		return strings.HasPrefix(value, "GIT_")
	})
	environment = append(environment, "GIT_TERMINAL_PROMPT=0")
	fixtureGit(t, checkout, environment, "init", "--quiet", "--object-format=sha1")
	fixtureGit(t, checkout, environment, "remote", "add", "origin", fixture.URL)
	fixtureGit(t, checkout, environment, "fetch", "--depth=1", "origin", fixture.Commit)
	fixtureGit(t, checkout, environment, "checkout", "--detach", fixture.Commit)
	fixtureGit(t, checkout, environment, "submodule", "update", "--init", "--recursive")
	// Lucy clones this detached HEAD, so revision selection belongs to the fixture runner.
	//
	// The checkout has to reach Lucy as a URL, and a URL path after the authority
	// always begins with a slash, so no spelling keeps a Windows drive letter in the
	// path. An empty host puts it in the host instead ("file://C:/...") and lucy
	// rejects that; naming localhost keeps it in the path as "/C:/...", which lucy
	// accepts and git resolves to the same directory.
	remote := (&url.URL{
		Scheme: "file",
		Host:   "localhost",
		Path:   filepath.ToSlash(checkout),
	}).String()
	args := []string{"compile", remote, "--output", filepath.Join(temporary, "artifact.jar"), "--no-style"}
	if fixture.Ecosystem != "" {
		args = append(args, "--ecosystem", fixture.Ecosystem)
	}
	if fixture.Project != "" {
		args = append(args, "--project", fixture.Project)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = temporary
	cmd.Env = environment
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lucy compile: %v\n%s", err, output)
	}
	t.Logf("compiled %s (%s @ %s)", fixture.ID, fixture.URL, fixture.Commit)
}

func fixtureGit(t *testing.T, dir string, environment []string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "protocol.ext.allow=never"}, args...)...)
	cmd.Dir = dir
	cmd.Env = environment
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
