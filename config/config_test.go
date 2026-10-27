package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/config"
	"gopkg.in/yaml.v3"
)

// newService isolates both configuration scopes in temporary directories.
func newService(t *testing.T) (*config.Service, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localappdata"))
	root := t.TempDir()
	service, err := config.New(root)
	if err != nil {
		t.Fatalf("config.New: %v", err)
	}
	return service, root
}

func workspacePath(root string) string {
	return filepath.Join(root, filepath.FromSlash(config.WorkspaceFilename))
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestCreateWritesDocumentedTemplate(t *testing.T) {
	service, root := newService(t)
	if _, err := service.Create(config.WorkspaceScope, config.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	text := readText(t, workspacePath(root))
	for _, documented := range []string{"java", "github", "curseforge", "home", "proxy", "api_key"} {
		if !strings.Contains(text, documented) {
			t.Errorf("template does not document %q:\n%s", documented, text)
		}
	}
	if !strings.Contains(text, "#") {
		t.Errorf("template has no comments:\n%s", text)
	}
	value, err := service.Get("github.proxy", config.WorkspaceScope)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if value != "" {
		t.Errorf("fresh template already sets github.proxy: %v", value)
	}
}

func TestSetPreservesUserComments(t *testing.T) {
	service, root := newService(t)
	if _, err := service.Create(config.WorkspaceScope, config.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := workspacePath(root)
	note := "# this note belongs to the machine owner"
	if err := os.WriteFile(path, []byte(note+"\n"+readText(t, path)), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}

	if _, err := service.Set("github.proxy", "https://mirror.example/", config.WorkspaceScope); err != nil {
		t.Fatalf("Set github.proxy: %v", err)
	}
	if _, err := service.Set("java.home", "/opt/java", config.WorkspaceScope); err != nil {
		t.Fatalf("Set java.home: %v", err)
	}

	text := readText(t, path)
	if count := strings.Count(text, note); count != 1 {
		t.Errorf("user comment appears %d times, want once:\n%s", count, text)
	}
	if value, _ := service.GitHubProxy(); value != "https://mirror.example/" {
		t.Errorf("GitHubProxy = %q", value)
	}
	if value, _ := service.Get("java.home", config.WorkspaceScope); value != "/opt/java" {
		t.Errorf("java.home = %v", value)
	}
}

func TestSetIsIdempotent(t *testing.T) {
	service, root := newService(t)
	if _, err := service.Create(config.WorkspaceScope, config.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := workspacePath(root)
	if _, err := service.Set("github.proxy", "https://mirror.example/", config.WorkspaceScope); err != nil {
		t.Fatalf("Set: %v", err)
	}
	before := readText(t, path)
	if _, err := service.Set("github.proxy", "https://mirror.example/", config.WorkspaceScope); err != nil {
		t.Fatalf("Set again: %v", err)
	}
	if after := readText(t, path); after != before {
		t.Errorf("repeating the same write changed the file:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestSetDocumentsNewKey(t *testing.T) {
	service, root := newService(t)
	if _, err := service.Create(config.WorkspaceScope, config.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Set("curseforge.api_key", "secret-key", config.WorkspaceScope); err != nil {
		t.Fatalf("Set: %v", err)
	}
	lines := strings.Split(readText(t, workspacePath(root)), "\n")
	index := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "api_key: secret-key" {
			index = i
		}
	}
	if index < 0 {
		t.Fatalf("written key not found in:\n%s", strings.Join(lines, "\n"))
	}
	if index == 0 || !strings.HasPrefix(strings.TrimSpace(lines[index-1]), "#") {
		t.Errorf("new key has no explanation above it:\n%s", strings.Join(lines, "\n"))
	}
}

func TestSetRejectsInvalidProxy(t *testing.T) {
	service, root := newService(t)
	if _, err := service.Create(config.WorkspaceScope, config.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := workspacePath(root)
	before := readText(t, path)
	for _, invalid := range []string{"not-a-url", "ftp://mirror.example/", "https://"} {
		if _, err := service.Set("github.proxy", invalid, config.WorkspaceScope); err == nil {
			t.Errorf("Set accepted invalid proxy %q", invalid)
		}
		if after := readText(t, path); after != before {
			t.Fatalf("invalid proxy %q changed the file", invalid)
		}
	}
}

func TestWorkspaceOverridesGlobal(t *testing.T) {
	service, _ := newService(t)
	global := "https://global.example/"
	workspace := "https://workspace.example/"
	if _, err := service.Set("github.proxy", global, config.GlobalScope); err != nil {
		t.Fatalf("Set global: %v", err)
	}
	if _, err := service.Set("github.proxy", workspace, config.WorkspaceScope); err != nil {
		t.Fatalf("Set workspace: %v", err)
	}

	if value, err := service.GitHubProxy(); err != nil || value != workspace {
		t.Errorf("GitHubProxy = %q, %v; want workspace value", value, err)
	}
	if value, err := service.Setting("github.proxy"); err != nil || value != workspace {
		t.Errorf("Setting = %q, %v; want workspace value", value, err)
	}
	if value, err := service.Get("github.proxy", config.GlobalScope); err != nil || value != global {
		t.Errorf("global Get = %v, %v; want %q", value, err, global)
	}
	if value, err := service.Get("github.proxy", config.WorkspaceScope); err != nil || value != workspace {
		t.Errorf("workspace Get = %v, %v; want %q", value, err, workspace)
	}
}

func TestCurseForgeKeyIsReadable(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.Set("curseforge.api_key", "secret-key", config.WorkspaceScope); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if value, err := service.CurseForgeAPIKey(); err != nil || value != "secret-key" {
		t.Errorf("CurseForgeAPIKey = %q, %v", value, err)
	}
	if value, err := service.Get("curseforge.api_key", config.WorkspaceScope); err != nil || value != "secret-key" {
		t.Errorf("Get = %v, %v", value, err)
	}
}

func TestCreateRefusesAndForceReplaces(t *testing.T) {
	service, root := newService(t)
	if _, err := service.Create(config.WorkspaceScope, config.CreateOptions{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Set("java.home", "/opt/java", config.WorkspaceScope); err != nil {
		t.Fatalf("Set: %v", err)
	}
	path := workspacePath(root)
	note := "# note before replacement"
	if err := os.WriteFile(path, []byte(note+"\n"+readText(t, path)), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}
	before := readText(t, path)

	if _, err := service.Create(config.WorkspaceScope, config.CreateOptions{}); err == nil {
		t.Fatal("Create replaced an existing file without --force")
	}
	if after := readText(t, path); after != before {
		t.Fatal("refused create changed the file")
	}

	if _, err := service.Create(config.WorkspaceScope, config.CreateOptions{Force: true}); err != nil {
		t.Fatalf("forced Create: %v", err)
	}
	if value, _ := service.Get("java.home", config.WorkspaceScope); value != "" {
		t.Errorf("forced create kept java.home = %v", value)
	}
	if strings.Contains(readText(t, path), note) {
		t.Error("forced create kept a comment from the replaced file")
	}
}

func TestCreateNoCommentsWritesPlainMapping(t *testing.T) {
	service, root := newService(t)
	if _, err := service.Create(config.WorkspaceScope, config.CreateOptions{NoComments: true}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	text := readText(t, workspacePath(root))
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			t.Errorf("comment-free create wrote a comment: %q", line)
		}
	}
	var document map[string]any
	if err := yaml.Unmarshal([]byte(text), &document); err != nil {
		t.Fatalf("decode plain config: %v", err)
	}
	if len(document) != 0 {
		t.Errorf("comment-free create activated settings: %v", document)
	}
}

func TestSetCreatesMissingFile(t *testing.T) {
	service, root := newService(t)
	if _, err := service.Set("curseforge.api_key", "secret-key", config.WorkspaceScope); err != nil {
		t.Fatalf("Set on missing file: %v", err)
	}
	text := readText(t, workspacePath(root))
	if !strings.Contains(text, "#") {
		t.Errorf("created file has no documentation comments:\n%s", text)
	}
	if value, _ := service.CurseForgeAPIKey(); value != "secret-key" {
		t.Errorf("CurseForgeAPIKey = %q", value)
	}
}

func TestSetPreservesHandWrittenComments(t *testing.T) {
	service, root := newService(t)
	path := workspacePath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create config directory: %v", err)
	}
	handWritten := "# hand written\njava:\n  home: /opt/java # my runtime\n"
	if err := os.WriteFile(path, []byte(handWritten), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := service.Set("github.proxy", "https://mirror.example/", config.WorkspaceScope); err != nil {
		t.Fatalf("Set: %v", err)
	}
	text := readText(t, path)
	for _, comment := range []string{"# hand written", "# my runtime"} {
		if !strings.Contains(text, comment) {
			t.Errorf("user comment %q did not survive:\n%s", comment, text)
		}
	}
	if value, _ := service.Get("java.home", config.WorkspaceScope); value != "/opt/java" {
		t.Errorf("java.home = %v", value)
	}
	if value, _ := service.GitHubProxy(); value != "https://mirror.example/" {
		t.Errorf("GitHubProxy = %q", value)
	}
}

func TestSettingRejectsUnknownKeys(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.Setting("unknown.key"); err == nil {
		t.Error("Setting accepted an unknown key")
	}
	if _, err := service.Setting("github"); err == nil {
		t.Error("Setting accepted a section as a string setting")
	}
}
