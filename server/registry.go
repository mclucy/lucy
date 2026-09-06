package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mclucy/lucy/state"
	"gopkg.in/yaml.v3"
)

// ReadInstance validates the name and loads a normalized registry entry.
// A missing entry returns nil without an error.
func ReadInstance(name string) (*Instance, error) {
	if err := ValidateInstanceName(name); err != nil {
		return nil, err
	}
	data, ok, err := state.SafeRead(InstanceRegistryPath(name))
	if err != nil || !ok {
		return nil, err
	}
	var inst Instance
	if err := yaml.Unmarshal(data, &inst); err != nil {
		return nil, fmt.Errorf("parse server registry %q: %w", name, err)
	}
	normalizeInstance(&inst)
	return &inst, nil
}

// WriteInstance validates and normalizes inst in place, then atomically replaces
// its registry entry, creating the registry directory if needed.
func WriteInstance(inst *Instance) error {
	if inst == nil {
		return fmt.Errorf("server instance is nil")
	}
	if err := ValidateInstanceName(inst.Name); err != nil {
		return err
	}
	normalizeInstance(inst)
	data, err := yaml.Marshal(inst)
	if err != nil {
		return fmt.Errorf("serialize server registry %q: %w", inst.Name, err)
	}
	if err := os.MkdirAll(ServersDir(), 0o755); err != nil {
		return fmt.Errorf("create server registry directory: %w", err)
	}
	return state.AtomicWrite(InstanceRegistryPath(inst.Name), data, 0o644)
}

// RemoveInstance removes only the validated registry entry; missing entries are
// accepted, and callers must separately stop and remove the native service.
func RemoveInstance(name string) error {
	if err := ValidateInstanceName(name); err != nil {
		return err
	}
	if err := os.Remove(InstanceRegistryPath(name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove server registry %q: %w", name, err)
	}
	return nil
}

// ListInstances reads YAML registry entries in name order, returning an empty list
// for a missing directory and an error if any entry cannot be read or parsed.
func ListInstances() ([]Instance, error) {
	entries, err := os.ReadDir(ServersDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read server registry directory: %w", err)
	}

	instances := make([]Instance, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		inst, err := ReadInstance(name)
		if err != nil {
			return nil, err
		}
		if inst != nil {
			instances = append(instances, *inst)
		}
	}
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].Name < instances[j].Name
	})
	return instances, nil
}

// FindInstanceForPath selects the most specific registered root containing path,
// or nil if none matches; registry read failures are returned to the caller.
func FindInstanceForPath(path string) (*Instance, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	instances, err := ListInstances()
	if err != nil {
		return nil, err
	}
	var best *Instance
	bestLen := -1
	for i := range instances {
		root := instances[i].Root
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if sameOrChild(absPath, absRoot) && len(absRoot) > bestLen {
			best = &instances[i]
			bestLen = len(absRoot)
		}
	}
	return best, nil
}

// sameOrChild checks containment after resolving symlinks where possible,
// falling back to the supplied paths when resolution fails.
func sameOrChild(path, root string) bool {
	pathEval, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = pathEval
	}
	rootEval, err := filepath.EvalSymlinks(root)
	if err == nil {
		root = rootEval
	}
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// normalizeInstance fills omitted registry metadata and service names in place
// without changing explicitly configured values.
func normalizeInstance(inst *Instance) {
	if inst.FormatVersion == "" {
		inst.FormatVersion = FormatVersion
	}
	if inst.RunUser == "" {
		inst.RunUser = DefaultRunUser
	}
	if inst.RuntimeConfig == "" && inst.Root != "" {
		inst.RuntimeConfig = filepath.Join(inst.Root, LocalConfigFile)
	}
	if inst.SystemdService == "" && inst.Name != "" {
		inst.SystemdService = SystemdInstanceServiceName(inst.Name)
	}
	if inst.LaunchdLabel == "" && inst.Name != "" {
		inst.LaunchdLabel = LaunchdInstanceLabel(inst.Name)
	}
}
