package init

import (
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/state"
)

func TestNewInitFlowState_EmptyWorkDir(t *testing.T) {
	tmpDir := t.TempDir()
	s := NewInitFlowState(tmpDir)

	if len(s.ExistingFiles) != 0 {
		t.Errorf("expected no existing files, got %v", s.ExistingFiles)
	}
}

func TestNewInitFlowState_WithExistingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	_ = filepath.Join(tmpDir, string(state.ConfigFile))
	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	s := NewInitFlowState(tmpDir)

	found := false
	for _, f := range s.ExistingFiles {
		if f == string(state.ConfigFile) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ExistingFiles to contain %s, got %v", state.ConfigFile, s.ExistingFiles)
	}
}

func TestBuildResult_PreserveExisting(t *testing.T) {
	tmpDir := t.TempDir()
	_ = filepath.Join(tmpDir, string(state.ConfigFile))
	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	s := NewInitFlowState(tmpDir)
	s.GameVersion = "1.21.4"
	s.ConflictResolution = PreserveExisting
	s.ManagedRoots = []string{"mods", "plugins"}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ConfigToWrite != nil {
		t.Error("expected ConfigToWrite to be nil when preserving existing")
	}

	skipped := false
	for _, f := range result.SkippedFiles {
		if f == string(state.ConfigFile) {
			skipped = true
			break
		}
	}
	if !skipped {
		t.Errorf("expected SkippedFiles to contain %s, got %v", state.ConfigFile, result.SkippedFiles)
	}
}

func TestBuildResult_AbortOnConflict(t *testing.T) {
	tmpDir := t.TempDir()
	_ = filepath.Join(tmpDir, string(state.ConfigFile))
	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	s := NewInitFlowState(tmpDir)
	s.GameVersion = "1.21.4"
	s.ConflictResolution = AbortOnConflict
	s.ManagedRoots = []string{"mods", "plugins"}

	_, err := BuildResult(s)
	if err == nil {
		t.Error("expected error when aborting on conflict with existing files")
	}

	conflictErr, ok := err.(*ErrConflict)
	if !ok {
		t.Fatalf("expected ErrConflict, got %T", err)
	}
	if conflictErr.Mode != AbortOnConflict {
		t.Errorf("expected mode AbortOnConflict, got %v", conflictErr.Mode)
	}
}

func TestBuildResult_OverwriteAll(t *testing.T) {
	tmpDir := t.TempDir()
	_ = filepath.Join(tmpDir, string(state.ConfigFile))
	cfg := state.ConfigDefaults()
	if err := state.WriteConfig(tmpDir, &cfg); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	s := NewInitFlowState(tmpDir)
	s.GameVersion = "1.21.4"
	s.ConflictResolution = OverwriteAll
	s.ManagedRoots = []string{"mods", "plugins"}

	result, err := BuildResult(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ConfigToWrite == nil {
		t.Error("expected ConfigToWrite to be set when OverwriteAll")
	}

	written := false
	for _, f := range result.WrittenFiles {
		if f == string(state.ConfigFile) {
			written = true
			break
		}
	}
	if !written {
		t.Errorf("expected WrittenFiles to contain %s, got %v", state.ConfigFile, result.WrittenFiles)
	}
}

func TestCanProceed_EmptyGameVersion(t *testing.T) {
	s := &InitFlowState{
		GameVersion:  "",
		ManagedRoots: []string{"mods", "plugins"},
	}

	if CanProceed(s) {
		t.Error("expected CanProceed to return false when GameVersion is empty")
	}
}

func TestCanProceed_EmptyManagedRoots(t *testing.T) {
	s := &InitFlowState{
		GameVersion:  "1.21.4",
		ManagedRoots: nil,
	}

	if CanProceed(s) {
		t.Error("expected CanProceed to return false when ManagedRoots is empty")
	}
}

func TestCanProceed_ValidState(t *testing.T) {
	s := &InitFlowState{
		GameVersion:  "1.21.4",
		ManagedRoots: []string{"mods", "plugins"},
	}

	if !CanProceed(s) {
		t.Error("expected CanProceed to return true for valid state")
	}
}

func TestCanProceed_ValidWithMultipleRoots(t *testing.T) {
	s := &InitFlowState{
		GameVersion:  "1.21.4",
		ManagedRoots: []string{"mods", "plugins", "config"},
	}

	if !CanProceed(s) {
		t.Error("expected CanProceed to return true for valid state with multiple roots")
	}
}

func TestConflictMode_String(t *testing.T) {
	tests := []struct {
		mode ConflictMode
		want string
	}{
		{PreserveExisting, "preserve"},
		{AbortOnConflict, "abort"},
		{OverwriteAll, "overwrite"},
	}

	for _, tc := range tests {
		if string(tc.mode) != tc.want {
			t.Errorf("expected %q, got %q", tc.want, tc.mode)
		}
	}
}
