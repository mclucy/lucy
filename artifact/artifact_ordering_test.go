package artifact

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestReadModulesFabricOrderIsStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.jar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	archive := zip.NewWriter(file)
	descriptor, err := archive.Create("fabric.mod.json")
	if err != nil {
		t.Fatalf("create descriptor: %v", err)
	}
	if _, err := descriptor.Write([]byte(`{
		"id": "demo",
		"version": "1.0.0",
		"provides": ["zeta", "alpha"],
		"depends": {"zeta": ">=1", "alpha": ">=2", "middle": "*"},
		"recommends": {"optional_b": "*", "optional_a": "*"}
	}`)); err != nil {
		t.Fatalf("write descriptor: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}

	var first []catalog.DescriptorModule
	for i := 0; i < 20; i++ {
		modules, err := ReadModules(context.Background(), path, catalog.EcosystemFabric)
		if err != nil {
			t.Fatalf("ReadModules() error = %v", err)
		}
		if i == 0 {
			first = modules
			continue
		}
		if !reflect.DeepEqual(first, modules) {
			t.Fatalf("ReadModules() order changed between reads:\nfirst: %+v\nnext:  %+v", first, modules)
		}
	}
	if len(first) != 1 {
		t.Fatalf("ReadModules() modules = %d, want 1", len(first))
	}
	if got, want := first[0].Provides, []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("provides = %v, want %v", got, want)
	}
	var required []string
	for _, dependency := range first[0].Dependencies {
		if dependency.Kind == catalog.DependencyKindRequired {
			required = append(required, dependency.ID)
		}
	}
	if want := []string{"alpha", "middle", "zeta"}; !reflect.DeepEqual(required, want) {
		t.Fatalf("required dependencies = %v, want %v", required, want)
	}
}
