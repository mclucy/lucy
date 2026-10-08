package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"

	"gopkg.in/yaml.v3"
)

type lockScenario struct {
	environment string
	packages    []packageExpectation
}

type packageExpectation struct {
	reference, projectID, releaseID, fileID, filename string
}

var lockScenarios = map[string]lockScenario{
	"vanilla-1201": {environment: "vanilla-1201"},
	"fabric-1214": {
		environment: "lock-fabric-1214",
		packages: []packageExpectation{
			{
				reference: "modrinth:fabric-api", projectID: "P7dR8mSH", releaseID: "p96k10UR",
				fileID: "9aQezMA2", filename: "fabric-api-0.119.4+1.21.4.jar",
			},
			{
				reference: "modrinth:flan", projectID: "Si383TIH", releaseID: "9EC25rAV",
				fileID: "HjsqW7BQ", filename: "flan-1.21.4-1.11.7.b-fabric.jar",
			},
		},
	},
}

func executeScenario(ctx context.Context, binary, sandboxes, id string) (resultErr error) {
	scenario, ok := lockScenarios[id]
	if !ok {
		return fmt.Errorf("unknown lock scenario %q", id)
	}
	fixture := filepath.Join(sandboxes, scenario.environment)
	if _, err := os.Stat(filepath.Join(fixture, "server.jar")); err != nil {
		return fmt.Errorf("generate %s with task envs:gen first: %w", scenario.environment, err)
	}
	intent, err := os.ReadFile(filepath.Join("e2e", "testdata", "lock", id+".yaml"))
	if err != nil {
		return fmt.Errorf("read scenario intent: %w", err)
	}
	var declared manifestIntent
	if err := yaml.Unmarshal(intent, &declared); err != nil {
		return fmt.Errorf("decode scenario intent: %w", err)
	}
	temporary, err := os.MkdirTemp("", "lucy-e2e-lock-"+id+"-*")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(temporary))
	}()
	root := filepath.Join(temporary, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		return err
	}
	runner := commandRunner{binary: binary, home: filepath.Join(temporary, "home")}
	if err := os.WriteFile(filepath.Join(root, "lucy.yaml"), intent, 0o644); err != nil {
		return err
	}
	for _, flag := range []string{"--locked", "--offline"} {
		if err := rejectInstall(ctx, runner, root, flag); err != nil {
			return fmt.Errorf("missing lock (%s): %w", flag, err)
		}
	}
	if _, err := runner.execute(ctx, root, "install"); err != nil {
		return fmt.Errorf("fresh generation: %w", err)
	}
	initial, err := readLock(root)
	if err != nil {
		return err
	}
	if err := scenario.verify(root, fixture, declared, initial.document); err != nil {
		return fmt.Errorf("fresh lock: %w", err)
	}
	outputs, err := fileDigests(filepath.Join(root, declared.Server.Directory))
	if err != nil {
		return err
	}
	for _, args := range [][]string{{"install"}, {"install", "--locked", "--offline"}} {
		if _, err := runner.execute(ctx, root, args...); err != nil {
			return err
		}
		if err := initial.unchanged(root); err != nil {
			return fmt.Errorf("repeat installation: %w", err)
		}
	}
	if err := verifyReplay(ctx, runner, root, initial); err != nil {
		return err
	}

	// Different YAML order, comments, and a normalized path must not change intent.
	var formatted map[string]any
	if err := yaml.Unmarshal(intent, &formatted); err != nil {
		return err
	}
	server := formatted["server"].(map[string]any)
	server["directory"] = "./" + declared.Server.Directory
	if err := writeIntent(root, formatted); err != nil {
		return err
	}
	if _, err := runner.execute(ctx, root, "install", "--locked", "--offline"); err != nil {
		return fmt.Errorf("equivalent manifest: %w", err)
	}
	if err := initial.unchanged(root); err != nil {
		return err
	}
	server["directory"] = "relocated"
	if err := writeIntent(root, formatted); err != nil {
		return err
	}
	for _, flag := range []string{"--locked", "--offline"} {
		if err := rejectInstall(ctx, runner, root, flag); err != nil {
			return fmt.Errorf("stale lock (%s): %w", flag, err)
		}
	}
	if _, err := runner.execute(ctx, root, "install"); err != nil {
		return fmt.Errorf("refresh stale lock: %w", err)
	}
	refreshed, err := readLock(root)
	if err != nil {
		return err
	}
	if refreshed.document.ManifestFingerprint == initial.document.ManifestFingerprint {
		return fmt.Errorf("changing the server directory did not update the manifest fingerprint")
	}
	declared.Server.Directory = "relocated"
	if err := scenario.verify(root, fixture, declared, refreshed.document); err != nil {
		return err
	}
	moved, err := fileDigests(filepath.Join(root, "relocated"))
	if err != nil {
		return err
	}
	if !maps.Equal(outputs, moved) {
		return fmt.Errorf("re-resolution changed pinned server outputs while relocating them")
	}
	previous, err := fileDigests(filepath.Join(root, "server"))
	if err != nil {
		return err
	}
	if len(previous) != 0 {
		return fmt.Errorf("relocation left owned files in the previous directory: %v", previous)
	}
	if err := verifyReplay(ctx, runner, root, refreshed); err != nil {
		return err
	}
	if len(scenario.packages) != 0 {
		if err := verifyDisabledRequirement(ctx, runner, root, formatted); err != nil {
			return err
		}
	}
	fmt.Printf("lock %s: fresh, repeat, manifest normalization, missing/stale guards, refresh, offline replay\n", id)
	return nil
}

func writeIntent(root string, intent map[string]any) error {
	data, err := yaml.Marshal(intent)
	if err != nil {
		return err
	}
	data = append([]byte("# Reordered manifest with unchanged declared intent.\n"), data...)
	return os.WriteFile(filepath.Join(root, "lucy.yaml"), data, 0o644)
}

func rejectInstall(ctx context.Context, runner commandRunner, root, flag string) error {
	before, err := fileDigests(root)
	if err != nil {
		return err
	}
	_, failure := runner.execute(ctx, root, "install", flag)
	if failure == nil {
		return fmt.Errorf("install %s accepted a missing or stale lock", flag)
	}
	exit, ok := errors.AsType[*exec.ExitError](failure)
	if !ok || exit.ExitCode() != 1 {
		return fmt.Errorf("expected a CLI rejection, not a process failure: %w", failure)
	}
	after, err := fileDigests(root)
	if err != nil {
		return err
	}
	if !maps.Equal(before, after) {
		return fmt.Errorf("rejected installation changed workspace files")
	}
	return nil
}

func verifyReplay(ctx context.Context, runner commandRunner, source string, locked lockSnapshot) error {
	replay, err := os.MkdirTemp(filepath.Dir(source), "replay-*")
	if err != nil {
		return err
	}
	cache := filepath.Join(replay, ".lucy", "downloads")
	if err := os.CopyFS(cache, os.DirFS(filepath.Join(source, ".lucy", "downloads"))); err != nil {
		return fmt.Errorf("copy warmed artifact cache: %w", err)
	}
	for _, name := range []string{"lucy.yaml", "lucy-lock.yaml"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(replay, name), data, 0o644); err != nil {
			return err
		}
	}
	want, err := fileDigests(filepath.Join(source, locked.document.Server.Directory))
	if err != nil {
		return err
	}
	runner.home = filepath.Join(replay, ".test-home")
	for _, lockOnly := range []bool{false, true} {
		if lockOnly {
			if err := os.Remove(filepath.Join(replay, "lucy.yaml")); err != nil {
				return err
			}
			if err := os.RemoveAll(filepath.Join(replay, locked.document.Server.Directory)); err != nil {
				return err
			}
			if err := os.RemoveAll(runner.home); err != nil {
				return err
			}
		}
		if _, err := runner.execute(ctx, replay, "install", "--locked", "--offline"); err != nil {
			return fmt.Errorf("offline reconstruction (lock-only=%t): %w", lockOnly, err)
		}
		if err := locked.unchanged(replay); err != nil {
			return err
		}
		got, err := fileDigests(filepath.Join(replay, locked.document.Server.Directory))
		if err != nil {
			return err
		}
		if !maps.Equal(want, got) {
			return fmt.Errorf("offline reconstruction changed pinned files or modes (lock-only=%t)", lockOnly)
		}
	}
	return nil
}

func verifyDisabledRequirement(ctx context.Context, runner commandRunner, root string, intent map[string]any) error {
	before, err := readLock(root)
	if err != nil {
		return err
	}
	server := intent["server"].(map[string]any)
	packages := server["packages"].(map[string]any)["fabric"].(map[string]any)
	packages["modrinth:flan"] = map[string]any{"version": packages["modrinth:flan"], "enabled": false}
	if err := writeIntent(root, intent); err != nil {
		return err
	}
	for _, flag := range []string{"--locked", "--offline"} {
		if err := rejectInstall(ctx, runner, root, flag); err != nil {
			return fmt.Errorf("changed requirement (%s): %w", flag, err)
		}
	}
	if _, err := runner.execute(ctx, root, "install"); err != nil {
		return err
	}
	after, err := readLock(root)
	if err != nil {
		return err
	}
	if before.document.ManifestFingerprint == after.document.ManifestFingerprint {
		return fmt.Errorf("disabling a requirement did not update the manifest fingerprint")
	}
	for _, old := range before.document.Packages {
		if old.ProjectID != "Si383TIH" {
			continue
		}
		path := filepath.Join(root, before.document.Server.Directory, "mods", old.Filename)
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("disabled Flan artifact was not pruned: %s (stat: %v)", path, err)
		}
	}
	var foundFlan, foundAPI bool
	for _, pkg := range after.document.Packages {
		if pkg.ProjectID == "Si383TIH" {
			foundFlan = true
			if len(pkg.Requirements) != 1 || pkg.Requirements[0].Reference != "modrinth:flan" || pkg.Requirements[0].Enabled {
				return fmt.Errorf("disabled Flan lost its declared reference or enabled state")
			}
			continue
		}
		if pkg.ProjectID == "P7dR8mSH" {
			foundAPI = true
			for _, old := range before.document.Packages {
				if old.ProjectID == pkg.ProjectID {
					old.Modules, pkg.Modules = nil, nil
					if !reflect.DeepEqual(old, pkg) {
						return fmt.Errorf("disabling Flan changed the pinned Fabric API selection")
					}
					path := filepath.Join(root, after.document.Server.Directory, "mods", pkg.Filename)
					if err := verifyLockedFile(path, pkg.lockedArtifact); err != nil {
						return err
					}
				}
			}
		}
	}
	if !foundFlan || !foundAPI {
		return fmt.Errorf("disabling Flan lost its binding or the still-required Fabric API artifact")
	}
	if _, err := runner.execute(ctx, root, "install", "--locked", "--offline"); err != nil {
		return err
	}
	return after.unchanged(root)
}
