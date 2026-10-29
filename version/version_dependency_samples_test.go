package version_test

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/version"
	"github.com/pelletier/go-toml"
)

func TestMcdrDependencyParsingFromSample(t *testing.T) {
	root := testDataRoot(t)
	pluginFile := filepath.Join(
		root,
		"dependency",
		"mcdr",
		"plugins",
		"PrimeBackup-v1.12.0",
		"mcdreforged.plugin.json",
	)

	var plugin fileschema.FileMcdrPluginIdentifier
	if err := json.Unmarshal(mustReadFile(t, pluginFile), &plugin); err != nil {
		t.Fatalf("unmarshal mcdr plugin metadata: %v", err)
	}

	rawRange, ok := plugin.Dependencies["mcdreforged"]
	if !ok {
		t.Fatalf("missing mcdreforged dependency in sample plugin metadata")
	}

	expr := mustParseMCDRRange(t, rawRange)
	assertConstraintSatisfy(
		t,
		expr,
		catalog.EcosystemMCDR,
		"mcdreforged",
		"2.12.0",
		true,
		"mcdr bound floor",
	)
	assertConstraintSatisfy(
		t,
		expr,
		catalog.EcosystemMCDR,
		"mcdreforged",
		"2.11.9",
		false,
		"mcdr below floor",
	)
}

func TestFabricDependencyParsingFromSample(t *testing.T) {
	root := testDataRoot(t)
	fabricMetaFile := filepath.Join(
		root,
		"dependency",
		"fabric",
		"mods",
		"fabric-carpet-1.21-1.4.147+v240613",
		"fabric.mod.json",
	)

	var mod fileschema.FileFabricModIdentifier
	if err := json.Unmarshal(
		mustReadFile(t, fabricMetaFile),
		&mod,
	); err != nil {
		t.Fatalf("unmarshal fabric mod metadata: %v", err)
	}

	minecraftExpr := mustParseFabricRanges(t, mod.Depends["minecraft"])
	assertConstraintSatisfy(
		t,
		minecraftExpr,
		catalog.EcosystemFabric,
		"minecraft",
		"1.20.2",
		true,
		"fabric >1.20.1 pass",
	)
	assertConstraintSatisfy(
		t,
		minecraftExpr,
		catalog.EcosystemFabric,
		"minecraft",
		"1.20.1",
		false,
		"fabric >1.20.1 reject",
	)

	loaderExpr := mustParseFabricRanges(t, mod.Depends["fabricloader"])
	assertConstraintSatisfy(
		t,
		loaderExpr,
		catalog.EcosystemFabric,
		"fabricloader",
		"0.16.9",
		true,
		"fabric loader >=0.14.18 pass",
	)
	assertConstraintSatisfy(
		t,
		loaderExpr,
		catalog.EcosystemFabric,
		"fabricloader",
		"0.14.17",
		false,
		"fabric loader >=0.14.18 reject",
	)
}

func TestForgeDependencyParsingFromSample(t *testing.T) {
	root := testDataRoot(t)
	ae2MetaFile := filepath.Join(
		root,
		"dependency",
		"forge",
		"mods",
		"appliedenergistics2-forge-15.2.13",
		"META-INF",
		"mods.toml",
	)

	var mod fileschema.FileModLoaderIdentifier
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

	expr := mustParseMavenRange(t, minecraftRange)
	assertConstraintSatisfy(
		t,
		expr,
		catalog.EcosystemForge,
		"minecraft",
		"1.20.1",
		true,
		"forge inclusive lower bound",
	)
	assertConstraintSatisfy(
		t,
		expr,
		catalog.EcosystemForge,
		"minecraft",
		"1.20.2",
		false,
		"forge exclusive upper bound",
	)
	assertConstraintSatisfy(
		t,
		expr,
		catalog.EcosystemForge,
		"minecraft",
		"1.20.0",
		false,
		"forge below lower bound",
	)

	yungsMetaFile := filepath.Join(
		root,
		"dependency",
		"forge",
		"mods",
		"YungsExtras-1.20-Forge-4.0.3",
		"META-INF",
		"mods.toml",
	)
	var yungs fileschema.FileModLoaderIdentifier
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

	yungsExpr := mustParseMavenRange(t, yungsApiRange)
	assertConstraintSatisfy(
		t,
		yungsExpr,
		catalog.EcosystemForge,
		"yungsapi",
		"1.20.0-Forge-4.0.0",
		false,
		"forge prerelease-style lower reject",
	)
	assertConstraintSatisfy(
		t,
		yungsExpr,
		catalog.EcosystemForge,
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

func mustParseMCDRRange(t *testing.T, raw string) version.Expr {
	t.Helper()
	expr, err := version.ParseConstraint(raw, version.DialectMCDR)
	if err != nil {
		t.Fatalf("parse MCDR range %q: %v", raw, err)
	}
	return expr
}

func mustParseMavenRange(t *testing.T, raw string) version.Expr {
	t.Helper()
	expr, err := version.ParseConstraint(raw, version.DialectMaven)
	if err != nil {
		t.Fatalf("parse Maven range %q: %v", raw, err)
	}
	return expr
}

func mustParseFabricRanges(
	t *testing.T,
	raw fn.SingleOrSlice[string],
) version.Expr {
	t.Helper()
	expr, err := version.ParseConstraints(raw, version.DialectFabric)
	if err != nil {
		t.Fatalf("parse Fabric ranges %v: %v", raw, err)
	}
	return expr
}

func dialectFor(platform catalog.Ecosystem) version.Dialect {
	switch platform {
	case catalog.EcosystemFabric:
		return version.DialectFabric
	case catalog.EcosystemMCDR:
		return version.DialectMCDR
	case catalog.EcosystemForge,
		catalog.EcosystemNeoForge,
		catalog.EcosystemSponge:
		return version.DialectMaven
	case catalog.EcosystemMinecraft:
		return version.DialectMinecraft
	default:
		return version.DialectOpaque
	}
}

func assertConstraintSatisfy(
	t *testing.T,
	expr version.Expr,
	platform catalog.Ecosystem,
	name string,
	rawVersion string,
	want bool,
	label string,
) {
	t.Helper()
	dialect, err := version.NewEngine(dialectFor(platform))
	if err != nil {
		t.Fatalf("dialect for %s: %v", platform, err)
	}
	actual, err := dialect.ParseVersion(version.Version(rawVersion))
	if err != nil {
		t.Fatalf("parse %s version %q: %v", platform, rawVersion, err)
	}
	got := dialect.Match(expr, actual)
	t.Logf(
		"%s: %s %s@%s => got=%v want=%v",
		label,
		platform,
		name,
		rawVersion,
		got,
		want,
	)
	if got != want {
		t.Fatalf(
			"constraint %+v satisfy(%s) = %v, want %v",
			expr,
			rawVersion,
			got,
			want,
		)
	}
}
