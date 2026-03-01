package detector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/types"
	"github.com/pelletier/go-toml"
)

func TestMcdrDependencyParsingFromSample(t *testing.T) {
	root := testDataRoot(t)
	pluginFile := filepath.Join(
		root,
		"mcdr",
		"plugins",
		"PrimeBackup-v1.12.0",
		"mcdreforged.plugin.json",
	)

	var plugin exttype.FileMcdrPluginIdentifier
	if err := json.Unmarshal(mustReadFile(t, pluginFile), &plugin); err != nil {
		t.Fatalf("unmarshal mcdr plugin metadata: %v", err)
	}

	rawRange, ok := plugin.Dependencies["mcdreforged"]
	if !ok {
		t.Fatalf("missing mcdreforged dependency in sample plugin metadata")
	}

	expr := parseNpmVersionRange(rawRange)
	if len(expr) != 1 || len(expr[0]) != 1 || expr[0][0].Operator != types.OpGte {
		t.Fatalf("unexpected parsed expression for %q: %+v", rawRange, expr)
	}

	assertConstraintSatisfy(
		t,
		expr,
		types.Mcdr,
		"mcdreforged",
		"2.12.0",
		true,
		"mcdr bound floor",
	)
	assertConstraintSatisfy(
		t,
		expr,
		types.Mcdr,
		"mcdreforged",
		"2.11.9",
		false,
		"mcdr below floor",
	)
}

// TODO: find a real-world example of Fabric mod with an array of version ranges to test the OR logic. For now we just test the parsing of this syntax with a synthetic example.
func TestFabricDependencyParsingFromSample(t *testing.T) {
	root := testDataRoot(t)
	fabricMetaFile := filepath.Join(
		root,
		"fabric",
		"mods",
		"fabric-carpet-1.21-1.4.147+v240613",
		"fabric.mod.json",
	)

	var mod exttype.FileFabricModIdentifier
	if err := json.Unmarshal(
		mustReadFile(t, fabricMetaFile),
		&mod,
	); err != nil {
		t.Fatalf("unmarshal fabric mod metadata: %v", err)
	}

	minecraftExpr := parseFabricVersionRanges(mod.Depends["minecraft"])
	assertConstraintSatisfy(
		t,
		minecraftExpr,
		types.Fabric,
		"minecraft",
		"1.20.2",
		true,
		"fabric >1.20.1 pass",
	)
	assertConstraintSatisfy(
		t,
		minecraftExpr,
		types.Fabric,
		"minecraft",
		"1.20.1",
		false,
		"fabric >1.20.1 reject",
	)

	loaderExpr := parseFabricVersionRanges(mod.Depends["fabricloader"])
	assertConstraintSatisfy(
		t,
		loaderExpr,
		types.Fabric,
		"fabricloader",
		"0.16.9",
		true,
		"fabric loader >=0.14.18 pass",
	)
	assertConstraintSatisfy(
		t,
		loaderExpr,
		types.Fabric,
		"fabricloader",
		"0.14.17",
		false,
		"fabric loader >=0.14.18 reject",
	)
}

func TestFabricVersionRangeArrayOR(t *testing.T) {
	const modJSON = `{
	  "schemaVersion": 1,
	  "id": "sample",
	  "version": "1.0.0",
	  "depends": {
	    "fabricloader": [">=0.14.18 <0.15.0", ">=0.16.0"]
	  }
	}`

	var mod exttype.FileFabricModIdentifier
	if err := json.Unmarshal([]byte(modJSON), &mod); err != nil {
		t.Fatalf("unmarshal fabric range-array sample: %v", err)
	}

	expr := parseFabricVersionRanges(mod.Depends["fabricloader"])
	if len(expr) != 2 {
		t.Fatalf("array OR should produce two OR clauses, got %d", len(expr))
	}

	assertConstraintSatisfy(
		t,
		expr,
		types.Fabric,
		"fabricloader",
		"0.14.19",
		true,
		"fabric array OR first branch",
	)
	assertConstraintSatisfy(
		t,
		expr,
		types.Fabric,
		"fabricloader",
		"0.15.2",
		false,
		"fabric array OR gap reject",
	)
	assertConstraintSatisfy(
		t,
		expr,
		types.Fabric,
		"fabricloader",
		"0.16.9",
		true,
		"fabric array OR second branch",
	)
}

func TestForgeDependencyParsingFromSample(t *testing.T) {
	root := testDataRoot(t)
	ae2MetaFile := filepath.Join(
		root,
		"forge",
		"mods",
		"appliedenergistics2-forge-15.2.13",
		"META-INF",
		"mods.toml",
	)

	var mod exttype.FileForgeModIdentifier
	if err := toml.Unmarshal(mustReadFile(t, ae2MetaFile), &mod); err != nil {
		t.Fatalf("unmarshal forge mods.toml: %v", err)
	}

	var minecraftRange string
	for _, dep := range mod.Dependencies["ae2"] {
		if dep.ModID == "minecraft" {
			minecraftRange = dep.VersionRange
			break
		}
	}
	if minecraftRange == "" {
		t.Fatalf("missing minecraft dependency range in ae2 sample")
	}

	expr := parseMavenVersionRange(minecraftRange)
	if len(expr) != 1 || len(expr[0]) != 2 {
		t.Fatalf(
			"expected one AND clause with two bounds for %q, got %+v",
			minecraftRange,
			expr,
		)
	}

	assertConstraintSatisfy(
		t,
		expr,
		types.Forge,
		"minecraft",
		"1.20.1",
		true,
		"forge inclusive lower bound",
	)
	assertConstraintSatisfy(
		t,
		expr,
		types.Forge,
		"minecraft",
		"1.20.2",
		false,
		"forge exclusive upper bound",
	)
	assertConstraintSatisfy(
		t,
		expr,
		types.Forge,
		"minecraft",
		"1.20.0",
		false,
		"forge below lower bound",
	)

	yungsMetaFile := filepath.Join(
		root,
		"forge",
		"mods",
		"YungsExtras-1.20-Forge-4.0.3",
		"META-INF",
		"mods.toml",
	)
	var yungs exttype.FileForgeModIdentifier
	if err := toml.Unmarshal(
		mustReadFile(t, yungsMetaFile),
		&yungs,
	); err != nil {
		t.Fatalf("unmarshal YUNG's Extras mods.toml: %v", err)
	}

	var yungsApiRange string
	for _, dep := range yungs.Dependencies["yungsextras"] {
		if dep.ModID == "yungsapi" {
			yungsApiRange = dep.VersionRange
			break
		}
	}
	if yungsApiRange == "" {
		t.Fatalf("missing yungsapi dependency range in YUNG's Extras sample")
	}

	yungsExpr := parseMavenVersionRange(yungsApiRange)
	if len(yungsExpr) == 0 {
		t.Fatalf("expected parsed expression for %q", yungsApiRange)
	}
	assertConstraintSatisfy(
		t,
		yungsExpr,
		types.Forge,
		"yungsapi",
		"1.20.0-Forge-4.0.0",
		false,
		"forge prerelease-style lower reject",
	)
	assertConstraintSatisfy(
		t,
		yungsExpr,
		types.Forge,
		"yungsapi",
		"1.20.0-Forge-4.0.1",
		true,
		"forge prerelease-style lower pass",
	)
}

func testDataRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("cannot locate test file path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "testdata"))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func mustParseSemver(t *testing.T, raw string) types.ComparableVersion {
	t.Helper()
	v := dependency.Parse(types.RawVersion(raw), types.Semver)
	if v == nil {
		t.Fatalf("parse semver %q failed", raw)
	}
	return v
}

func assertConstraintSatisfy(
	t *testing.T,
	expr types.VersionConstraintExpression,
	platform types.Platform,
	name string,
	version string,
	want bool,
	label string,
) {
	t.Helper()
	id := types.PackageId{Platform: platform, Name: types.ProjectName(name)}
	depSpec := types.Dependency{Id: id, Constraint: expr, Mandatory: true}
	got := depSpec.Satisfy(id, mustParseSemver(t, version))
	t.Logf(
		"%s: %s %s@%s => got=%v want=%v",
		label,
		platform,
		name,
		version,
		got,
		want,
	)
	if got != want {
		t.Fatalf(
			"constraint %+v satisfy(%s) = %v, want %v",
			expr,
			version,
			got,
			want,
		)
	}
}
