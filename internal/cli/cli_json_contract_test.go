package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestJSONContractsResolve(t *testing.T) {
	contracts := NewJSONContracts()
	root := &cobra.Command{Use: "lucy"}
	add := NewAddCommand(contracts)
	status := NewStatusCommand(contracts)
	config := NewConfigCommand(contracts)
	root.AddCommand(add, status, config)

	if !contracts.Supports(add) || !contracts.Supports(status) {
		t.Fatal("bound commands must support JSON output")
	}
	if contracts.Supports(root) {
		t.Fatal("unbound commands must not support JSON output")
	}

	paths := contracts.Paths(root)
	for _, want := range []string{
		"add",
		"status",
		"config get",
		"config set",
		"config create",
	} {
		if !slices.Contains(paths, want) {
			t.Fatalf("Paths = %v, missing %q", paths, want)
		}
	}
	if !slices.IsSorted(paths) {
		t.Fatalf("Paths must be sorted, got %v", paths)
	}
	for _, path := range paths {
		found, remaining, err := root.Find(strings.Fields(path))
		if err != nil || len(remaining) != 0 || found == nil {
			t.Fatalf("path %q does not resolve: command=%v remaining=%v err=%v",
				path, found, remaining, err)
		}
		schema, err := contracts.Schema(found)
		if err != nil {
			t.Fatalf("schema for %q: %v", path, err)
		}
		if schema == nil {
			t.Fatalf("schema for %q is nil", path)
		}
	}

	schema, err := contracts.Schema(add)
	if err != nil {
		t.Fatalf("schema for add: %v", err)
	}
	if want := "lucy add JSON result"; schema.Title != want {
		t.Fatalf("title = %q, want %q", schema.Title, want)
	}

	var configGet *cobra.Command
	for _, child := range config.Commands() {
		if child.Name() == "get" {
			configGet = child
		}
	}
	if configGet == nil {
		t.Fatal("config get command not found")
	}

	contracts.Ignore(config)
	if !contracts.Ignores(configGet) {
		t.Fatal("Ignore must propagate from a parent to its child")
	}
	if contracts.Ignores(add) {
		t.Fatal("Ignore must not leak to unrelated commands")
	}
}
