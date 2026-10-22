package version

import "testing"

func TestDialectMatching(t *testing.T) {
	cases := []struct {
		name       string
		dialect    Dialect
		constraint string
		actual     string
		want       bool
	}{
		{
			name:       "fabric range includes prerelease",
			dialect:    DialectFabric,
			constraint: ">=1.2.3- <1.2.4",
			actual:     "1.2.3-alpha",
			want:       true,
		},
		{
			name:       "fabric caret excludes next major prerelease",
			dialect:    DialectFabric,
			constraint: "^1.2.3",
			actual:     "2.0.0-alpha",
			want:       false,
		},
		{
			name:       "fabric wildcard includes lower prerelease",
			dialect:    DialectFabric,
			constraint: "1.2.x",
			actual:     "1.2.0-alpha",
			want:       true,
		},
		{
			name:       "fabric tilde one component",
			dialect:    DialectFabric,
			constraint: "~1",
			actual:     "1.1.0",
			want:       false,
		},
		{
			name:       "fabric repeated trailing wildcard",
			dialect:    DialectFabric,
			constraint: "1.x.x",
			actual:     "1.5.0",
			want:       true,
		},
		{
			name:       "fabric deep wildcard excludes next component prerelease",
			dialect:    DialectFabric,
			constraint: "1.2.3.x",
			actual:     "1.2.4-alpha",
			want:       false,
		},
		{
			name:       "mcdr range",
			dialect:    DialectMCDR,
			constraint: ">=2.0.0 <3.0.0",
			actual:     "2.12.0",
			want:       true,
		},
		{
			name:       "mcdr tilde keeps minor",
			dialect:    DialectMCDR,
			constraint: "~1",
			actual:     "1.1.0",
			want:       false,
		},
		{
			name:       "mcdr wildcard bound",
			dialect:    DialectMCDR,
			constraint: ">=1.0.x",
			actual:     "1.0.5",
			want:       true,
		},
		{
			name:       "maven range",
			dialect:    DialectMaven,
			constraint: "[1.0,2.0)",
			actual:     "1.9.0",
			want:       true,
		},
		{
			name:       "maven bare version is recommended",
			dialect:    DialectMaven,
			constraint: "1.0",
			actual:     "1.0.1",
			want:       true,
		},
		{
			name:       "maven qualifier order",
			dialect:    DialectMaven,
			constraint: "[1.0-alpha,1.0)",
			actual:     "1.0-beta",
			want:       true,
		},
		{
			name:       "minecraft snapshot",
			dialect:    DialectMinecraft,
			constraint: "24w14a",
			actual:     "24w14a",
			want:       true,
		},
		{
			name:       "opaque exact",
			dialect:    DialectOpaque,
			constraint: "1.2.3",
			actual:     "1.2.3",
			want:       true,
		},
		{
			name:       "opaque mismatch",
			dialect:    DialectOpaque,
			constraint: "1.2.3",
			actual:     "1.2.4",
			want:       false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dialect, err := NewEngine(testCase.dialect)
			if err != nil {
				t.Fatalf("dialect: %v", err)
			}
			got, err := dialect.MatchText(testCase.constraint, Version(testCase.actual))
			if err != nil {
				t.Fatalf("match %q: %v", testCase.constraint, err)
			}
			if got != testCase.want {
				t.Fatalf("match %q against %q = %v, want %v",
					testCase.constraint, testCase.actual, got, testCase.want)
			}
		})
	}
}

func TestConstraintErrorsAndNegation(t *testing.T) {
	unconstrained := []struct {
		dialect Dialect
		raw     string
	}{
		{DialectFabric, "*"},
		{DialectMCDR, ""},
		{DialectMaven, "none"},
		{DialectMaven, "1.0"},
		{DialectMinecraft, "*"},
		{DialectOpaque, "*"},
	}
	for _, item := range unconstrained {
		expr, err := ParseConstraint(item.raw, item.dialect)
		if err != nil || len(expr) != 0 {
			t.Fatalf("%s constraint %q must be unconstrained: expr=%v err=%v",
				item.dialect, item.raw, expr, err)
		}
	}

	invalid := []struct {
		dialect Dialect
		raw     string
	}{
		{DialectFabric, ">=1.0.0 <"},
		{DialectMCDR, ">=1.0.0 <"},
		{DialectMaven, "[1.0"},
		{DialectMaven, "[2.0,1.0]"},
		{DialectMinecraft, "not-a-version"},
		{DialectOpaque, ">=1.0"},
	}
	for _, item := range invalid {
		if _, err := ParseConstraint(item.raw, item.dialect); err == nil {
			t.Fatalf("%s constraint %q must be rejected", item.dialect, item.raw)
		}
	}

	dialect, err := NewEngine(DialectFabric)
	if err != nil {
		t.Fatalf("dialect: %v", err)
	}
	expr, err := dialect.ParseConstraint("=1.0.0 || =2.0.0")
	if err != nil {
		t.Fatalf("parse alternatives: %v", err)
	}
	negated := expr.Negate()
	for raw, want := range map[string]bool{
		"1.5.0": true,
		"1.0.0": false,
		"2.0.0": false,
	} {
		actual, err := dialect.ParseVersion(Version(raw))
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got := dialect.Match(negated, actual); got != want {
			t.Fatalf("negated match %q = %v, want %v", raw, got, want)
		}
	}

	fabricVersion, err := ParseVersion("1.0.0", DialectFabric)
	if err != nil {
		t.Fatalf("parse fabric: %v", err)
	}
	mcdrVersion, err := ParseVersion("1.0.0", DialectMCDR)
	if err != nil {
		t.Fatalf("parse mcdr: %v", err)
	}
	if _, comparable := Compare(fabricVersion, mcdrVersion); comparable {
		t.Fatal("cross-dialect versions must be incomparable")
	}

	plainVersion, err := ParseVersion("v1", DialectFabric)
	if err != nil {
		t.Fatalf("parse plain fabric: %v", err)
	}
	exclusive, err := dialect.ParseConstraint("<2.0")
	if err != nil {
		t.Fatalf("exclusive constraint: %v", err)
	}
	if dialect.Match(exclusive, plainVersion) {
		t.Fatal("exclusive Fabric predicate must not match a plain string")
	}
	inverted, err := dialect.ParseConstraint("=2.0")
	if err != nil {
		t.Fatalf("equality constraint: %v", err)
	}
	if !dialect.Match(inverted.Negate(), plainVersion) {
		t.Fatal("negated equality must match a different plain string")
	}
}

func TestOpaqueVersionsAreEqualityOnly(t *testing.T) {
	first, err := ParseVersion("1.0", DialectOpaque)
	if err != nil {
		t.Fatalf("parse first: %v", err)
	}
	second, err := ParseVersion("2.0", DialectOpaque)
	if err != nil {
		t.Fatalf("parse second: %v", err)
	}
	if _, comparable := Compare(first, second); comparable {
		t.Fatal("opaque versions must not be ordered")
	}

	dialect, err := NewEngine(DialectOpaque)
	if err != nil {
		t.Fatalf("dialect: %v", err)
	}
	constraint, err := dialect.ParseConstraint("2.0")
	if err != nil {
		t.Fatalf("constraint: %v", err)
	}
	if dialect.Match(constraint, first) {
		t.Fatal("opaque exact constraint must not match another identifier")
	}
	if !dialect.Match(constraint, second) {
		t.Fatal("opaque exact constraint must match the same identifier")
	}
}

func TestNegatedIncomparablePredicates(t *testing.T) {
	cases := []struct {
		name       string
		dialect    Dialect
		constraint string
		actual     string
	}{
		{
			name:       "opaque different identifier",
			dialect:    DialectOpaque,
			constraint: "2.0",
			actual:     "1.0",
		},
		{
			name:       "minecraft across release lines",
			dialect:    DialectMinecraft,
			constraint: "1.21.4",
			actual:     "26.1",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dialect, err := NewEngine(testCase.dialect)
			if err != nil {
				t.Fatalf("dialect: %v", err)
			}
			actual, err := dialect.ParseVersion(Version(testCase.actual))
			if err != nil {
				t.Fatalf("parse actual %q: %v", testCase.actual, err)
			}
			if _, comparable := dialect.Compare(actual, actual); !comparable {
				t.Fatalf("%q must be comparable with itself", testCase.actual)
			}
			expr, err := dialect.ParseConstraint(testCase.constraint)
			if err != nil {
				t.Fatalf("parse constraint %q: %v", testCase.constraint, err)
			}
			bound := expr[0][0].Value
			if _, comparable := dialect.Compare(actual, bound); comparable {
				t.Fatalf("%q and %q must be incomparable", testCase.actual, testCase.constraint)
			}
			if dialect.Match(expr, actual) {
				t.Fatal("equality must not match an incomparable version")
			}
			if !dialect.Match(expr.Negate(), actual) {
				t.Fatal("negated equality must match an incomparable version")
			}
		})
	}
}

func TestZeroVersionIsValid(t *testing.T) {
	version, err := ParseVersion("0.0.0", DialectFabric)
	if err != nil {
		t.Fatalf("0.0.0 must be a valid semantic version: %v", err)
	}
	if !version.Validate() {
		t.Fatal("0.0.0 must validate")
	}
}
