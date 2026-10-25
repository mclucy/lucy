package version_test

// The fuzz targets below derive inclusion and ordering expectations from the
// documented grammars instead of inspecting parsed expression shapes. Numeric
// components are compared lexicographically through numericTriple, which makes
// interval membership independent of the package's internal representation.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/version"
)

const (
	fabricComponentLimit = 100_000
	mavenComponentLimit  = 10_000
)

var (
	fabricEqualityComparators = [...]string{">=", ">", "=", "!="}
	fabricRangeComparators    = [...]string{">=", ">", "<=", "<"}
	mavenQualifierLadder      = [...]string{
		"alpha", "beta", "milestone", "rc", "cr", "snapshot", "",
		"final", "ga", "release", "sp", "foo", "zeta",
	}
	mavenQualifierRanks = map[string]int{
		"alpha": 0, "beta": 1, "milestone": 2,
		"rc": 3, "cr": 3, "snapshot": 4,
		"": 5, "final": 5, "ga": 5, "release": 5,
		"sp": 6,
	}
)

type numericTriple [3]uint64

func newNumericTriple(
	limit, first, second, third uint64,
) numericTriple {
	return numericTriple{
		first % limit,
		second % limit,
		third % limit,
	}
}

func (triple numericTriple) String() string {
	return fmt.Sprintf("%d.%d.%d", triple[0], triple[1], triple[2])
}

func (triple numericTriple) compare(other numericTriple) int {
	return slices.Compare(triple[:], other[:])
}

func normalizedSign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}

func comparatorHolds(comparison int, operator string) bool {
	switch operator {
	case ">=":
		return comparison >= 0
	case ">":
		return comparison > 0
	case "<=":
		return comparison <= 0
	case "<":
		return comparison < 0
	case "=":
		return comparison == 0
	case "!=":
		return comparison != 0
	default:
		return false
	}
}

// FuzzFabricConstraintMatching checks Fabric predicates against independently
// computed membership: single comparators, conjunctions, hyphen ranges,
// wildcards, caret/tilde windows, raw disjunctions, and array alternatives
// (including the gap between an AND range and the following floor).
func FuzzFabricConstraintMatching(f *testing.F) {
	f.Add(uint8(0), uint64(1), uint64(2), uint64(3), uint64(2), uint64(0), uint64(0), uint64(1), uint64(2), uint64(4), uint8(0))
	f.Add(uint8(0), uint64(1), uint64(2), uint64(3), uint64(2), uint64(0), uint64(0), uint64(1), uint64(2), uint64(3), uint8(2))
	f.Add(uint8(0), uint64(1), uint64(2), uint64(3), uint64(2), uint64(0), uint64(0), uint64(1), uint64(2), uint64(3), uint8(3))
	f.Add(uint8(1), uint64(1), uint64(2), uint64(3), uint64(2), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint8(0))
	f.Add(uint8(1), uint64(1), uint64(2), uint64(3), uint64(2), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint8(2))
	f.Add(uint8(2), uint64(1), uint64(2), uint64(3), uint64(2), uint64(0), uint64(0), uint64(1), uint64(9), uint64(9), uint8(0))
	f.Add(uint8(2), uint64(2), uint64(0), uint64(0), uint64(1), uint64(0), uint64(0), uint64(1), uint64(5), uint64(0), uint8(0))
	f.Add(uint8(3), uint64(1), uint64(2), uint64(3), uint64(0), uint64(0), uint64(0), uint64(1), uint64(9), uint64(9), uint8(0))
	f.Add(uint8(3), uint64(1), uint64(2), uint64(3), uint64(0), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint8(0))
	f.Add(uint8(3), uint64(0), uint64(2), uint64(3), uint64(0), uint64(0), uint64(0), uint64(0), uint64(9), uint64(9), uint8(0))
	f.Add(uint8(3), uint64(0), uint64(2), uint64(3), uint64(0), uint64(0), uint64(0), uint64(1), uint64(0), uint64(0), uint8(0))
	f.Add(uint8(4), uint64(1), uint64(2), uint64(3), uint64(0), uint64(0), uint64(0), uint64(1), uint64(2), uint64(99), uint8(0))
	f.Add(uint8(4), uint64(1), uint64(2), uint64(3), uint64(0), uint64(0), uint64(0), uint64(1), uint64(3), uint64(0), uint8(0))
	f.Add(uint8(5), uint64(1), uint64(2), uint64(0), uint64(0), uint64(0), uint64(0), uint64(1), uint64(2), uint64(7), uint8(0))
	f.Add(uint8(5), uint64(1), uint64(2), uint64(0), uint64(0), uint64(0), uint64(0), uint64(1), uint64(3), uint64(0), uint8(0))
	f.Add(uint8(6), uint64(1), uint64(0), uint64(0), uint64(3), uint64(0), uint64(0), uint64(5), uint64(0), uint64(0), uint8(8))
	f.Add(uint8(7), uint64(0), uint64(14), uint64(18), uint64(0), uint64(15), uint64(0), uint64(0), uint64(14), uint64(19), uint8(0))
	f.Add(uint8(7), uint64(0), uint64(14), uint64(18), uint64(0), uint64(15), uint64(0), uint64(0), uint64(15), uint64(2), uint8(0))
	f.Add(uint8(7), uint64(0), uint64(14), uint64(18), uint64(0), uint64(15), uint64(0), uint64(0), uint64(16), uint64(9), uint8(0))
	f.Add(uint8(7), uint64(2), uint64(0), uint64(0), uint64(1), uint64(0), uint64(0), uint64(1), uint64(5), uint64(0), uint8(0))
	f.Add(uint8(1), uint64(0), uint64(0), uint64(0), uint64(99_999), uint64(99_999), uint64(99_999), uint64(0), uint64(0), uint64(0), uint8(0))

	f.Fuzz(func(
		t *testing.T,
		form uint8,
		lo0, lo1, lo2, hi0, hi1, hi2, cand0, cand1, cand2 uint64,
		flags uint8,
	) {
		lo := newNumericTriple(fabricComponentLimit, lo0, lo1, lo2)
		hi := newNumericTriple(fabricComponentLimit, hi0, hi1, hi2)
		candidate := newNumericTriple(
			fabricComponentLimit,
			cand0, cand1, cand2,
		)
		lowerComparison := candidate.compare(lo)
		upperComparison := candidate.compare(hi)

		var (
			raw          string
			alternatives []string
			want         bool
		)
		switch form % 8 {
		case 0:
			operator := fabricEqualityComparators[flags%4]
			raw = operator + lo.String()
			want = comparatorHolds(lowerComparison, operator)
		case 1:
			lowerOperator := fabricEqualityComparators[flags%2]
			upperOperator := fabricRangeComparators[2+(flags/2)%2]
			raw = lowerOperator + lo.String() + " " +
				upperOperator + hi.String()
			want = comparatorHolds(lowerComparison, lowerOperator) &&
				comparatorHolds(upperComparison, upperOperator)
		case 2:
			raw = lo.String() + " - " + hi.String()
			want = lowerComparison >= 0 && upperComparison <= 0
		case 3:
			// Fabric defines ^a.b.c as >=a.b.c <(a+1).0.0 for every
			// major, including 0.x. Do not apply npm's 0.x caret rule.
			raw = "^" + lo.String()
			want = lowerComparison >= 0 && candidate[0] == lo[0]
		case 4:
			raw = "~" + lo.String()
			want = lowerComparison >= 0 &&
				candidate[0] == lo[0] && candidate[1] == lo[1]
		case 5:
			raw = fmt.Sprintf("%d.%d.x", lo[0], lo[1])
			want = candidate[0] == lo[0] && candidate[1] == lo[1]
		case 6:
			lowerOperator := fabricRangeComparators[flags%4]
			upperOperator := fabricRangeComparators[(flags/4)%4]
			raw = lowerOperator + lo.String() + " || " +
				upperOperator + hi.String()
			want = comparatorHolds(lowerComparison, lowerOperator) ||
				comparatorHolds(upperComparison, upperOperator)
		default:
			// Array alternatives where the first range is an AND clause.
			// The second range starts one minor above the first upper
			// bound, so candidate versions in the gap must not match.
			nextMinor := numericTriple{hi[0], hi[1] + 1, 0}
			alternatives = []string{
				">=" + lo.String() + " <" + hi.String(),
				">=" + nextMinor.String(),
			}
			raw = fmt.Sprintf("%q", alternatives)
			want = (lowerComparison >= 0 && upperComparison < 0) ||
				candidate.compare(nextMinor) >= 0
		}

		engine, err := version.NewEngine(version.DialectFabric)
		if err != nil {
			t.Fatalf("new Fabric engine: %v", err)
		}
		actual, err := engine.ParseVersion(version.Version(candidate.String()))
		if err != nil {
			t.Fatalf("parse candidate %s: %v", candidate, err)
		}

		var expr version.Expr
		if alternatives != nil {
			expr, err = engine.ParseConstraints(alternatives)
		} else {
			expr, err = engine.ParseConstraint(raw)
		}
		if err != nil {
			t.Fatalf("parse generated Fabric constraint %s: %v", raw, err)
		}

		if got := engine.Match(expr, actual); got != want {
			t.Fatalf(
				"Fabric constraint %s matches %s = %v, want %v",
				raw, candidate, got, want,
			)
		}
		if negated := engine.Match(expr.Negate(), actual); negated == want {
			t.Fatalf(
				"negated Fabric constraint %s matches %s = %v, want %v",
				raw, candidate, negated, !want,
			)
		}
	})
}

func mavenWithinRange(
	candidate, lower, upper numericTriple,
	includeLower, includeUpper bool,
) bool {
	lowerComparison := candidate.compare(lower)
	upperComparison := candidate.compare(upper)
	if includeLower {
		if lowerComparison < 0 {
			return false
		}
	} else if lowerComparison <= 0 {
		return false
	}
	if includeUpper {
		return upperComparison <= 0
	}
	return upperComparison < 0
}

func compareMavenQualifiers(left, right string) int {
	leftRank, leftKnown := mavenQualifierRanks[left]
	rightRank, rightKnown := mavenQualifierRanks[right]
	switch {
	case leftKnown && rightKnown:
		return normalizedSign(leftRank - rightRank)
	case leftKnown:
		return -1
	case rightKnown:
		return 1
	default:
		return strings.Compare(left, right)
	}
}

// FuzzMavenConstraintMatching checks Maven bracket ranges and qualifier
// ordering. Numeric ranges use independent interval arithmetic; qualifiers use
// Maven's documented alpha < beta < milestone < rc/cr < snapshot < release <
// sp ladder, with unknown qualifiers ordered after known ones.
func FuzzMavenConstraintMatching(f *testing.F) {
	f.Add(uint8(0), uint64(1), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint64(1), uint64(5), uint64(0), uint8(0), uint8(0))
	f.Add(uint8(1), uint64(1), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint64(1), uint64(0), uint64(0), uint8(0), uint8(0))
	f.Add(uint8(2), uint64(1), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint8(0), uint8(0))
	f.Add(uint8(3), uint64(1), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint8(0), uint8(0))
	f.Add(uint8(4), uint64(1), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint64(1), uint64(5), uint64(0), uint8(0), uint8(0))
	f.Add(uint8(4), uint64(1), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint64(2), uint64(5), uint64(0), uint8(0), uint8(0))
	f.Add(uint8(0), uint64(2), uint64(0), uint64(0), uint64(1), uint64(0), uint64(0), uint64(1), uint64(5), uint64(0), uint8(0), uint8(0))
	f.Add(uint8(1), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint8(0), uint8(0))
	f.Add(uint8(3), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint64(1), uint8(0), uint8(0))
	f.Add(uint8(7), uint64(1), uint64(0), uint64(0), uint64(2), uint64(0), uint64(0), uint64(2), uint64(0), uint64(1), uint8(0), uint8(0))
	f.Add(uint8(8), uint64(1), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint8(0), uint8(1))
	f.Add(uint8(8), uint64(1), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint8(7), uint8(10))
	f.Add(uint8(8), uint64(1), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint8(7), uint8(9))
	f.Add(uint8(8), uint64(1), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint8(11), uint8(10))
	f.Add(uint8(8), uint64(1), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint64(0), uint8(3), uint8(4))

	f.Fuzz(func(
		t *testing.T,
		mode uint8,
		lo0, lo1, lo2, hi0, hi1, hi2, cand0, cand1, cand2 uint64,
		qualifierLeft, qualifierRight uint8,
	) {
		engine, err := version.NewEngine(version.DialectMaven)
		if err != nil {
			t.Fatalf("new Maven engine: %v", err)
		}
		if mode&8 != 0 {
			fuzzMavenQualifierOrdering(
				t,
				engine,
				newNumericTriple(mavenComponentLimit, lo0, lo1, lo2),
				qualifierLeft,
				qualifierRight,
			)
			return
		}

		lo := newNumericTriple(mavenComponentLimit, lo0, lo1, lo2)
		hi := newNumericTriple(mavenComponentLimit, hi0, hi1, hi2)
		candidate := newNumericTriple(
			mavenComponentLimit,
			cand0, cand1, cand2,
		)

		left := "["
		if mode&1 == 0 {
			left = "("
		}
		right := "]"
		if mode&2 == 0 {
			right = ")"
		}
		raw := left + lo.String() + "," + hi.String() + right

		want := mavenWithinRange(
			candidate,
			lo,
			hi,
			left == "[",
			right == "]",
		)
		if mode&4 != 0 {
			secondLo := hi
			secondLo[2]++
			secondHi := secondLo
			secondHi[2]++
			raw += "," + left + secondLo.String() + "," +
				secondHi.String() + right
			want = want || mavenWithinRange(
				candidate,
				secondLo,
				secondHi,
				left == "[",
				right == "]",
			)
		}

		expr, err := engine.ParseConstraint(raw)
		boundsComparison := lo.compare(hi)
		invalidBounds := boundsComparison > 0 ||
			(boundsComparison == 0 && (left == "(" || right == ")"))
		if invalidBounds {
			if err == nil {
				t.Fatalf("Maven constraint %s must be rejected", raw)
			}
			return
		}
		if err != nil {
			t.Fatalf("parse generated Maven constraint %s: %v", raw, err)
		}
		actual, err := engine.ParseVersion(version.Version(candidate.String()))
		if err != nil {
			t.Fatalf("parse Maven candidate %s: %v", candidate, err)
		}

		if got := engine.Match(expr, actual); got != want {
			t.Fatalf(
				"Maven constraint %s matches %s = %v, want %v",
				raw, candidate, got, want,
			)
		}
		if negated := engine.Match(expr.Negate(), actual); negated == want {
			t.Fatalf(
				"negated Maven constraint %s matches %s = %v, want %v",
				raw, candidate, negated, !want,
			)
		}
	})
}

func fuzzMavenQualifierOrdering(
	t *testing.T,
	engine version.Engine,
	core numericTriple,
	qualifierLeft, qualifierRight uint8,
) {
	t.Helper()
	leftQualifier := mavenQualifierLadder[int(qualifierLeft)%len(mavenQualifierLadder)]
	rightQualifier := mavenQualifierLadder[int(qualifierRight)%len(mavenQualifierLadder)]
	leftText := core.String() + mavenQualifierSuffix(leftQualifier)
	rightText := core.String() + mavenQualifierSuffix(rightQualifier)

	leftVersion, err := engine.ParseVersion(version.Version(leftText))
	if err != nil {
		t.Fatalf("parse Maven version %q: %v", leftText, err)
	}
	rightVersion, err := engine.ParseVersion(version.Version(rightText))
	if err != nil {
		t.Fatalf("parse Maven version %q: %v", rightText, err)
	}

	want := compareMavenQualifiers(leftQualifier, rightQualifier)
	got, comparable := engine.Compare(leftVersion, rightVersion)
	if !comparable {
		t.Fatalf("Maven versions %q and %q must be comparable", leftText, rightText)
	}
	if normalizedSign(got) != want {
		t.Fatalf(
			"Compare(%q, %q) = %d, want sign %d",
			leftText, rightText, got, want,
		)
	}
}

func mavenQualifierSuffix(qualifier string) string {
	if qualifier == "" {
		return ""
	}
	return "-" + qualifier
}

const (
	minecraftPre26Release uint8 = iota
	minecraftPost26Release
	minecraftPre26Snapshot
	minecraftPost26Snapshot
)

type minecraftFixture struct {
	text     string
	family   uint8
	ordering [5]uint8
}

func newMinecraftFixture(
	kind, first, second, third, prerelease, number uint8,
) minecraftFixture {
	switch kind % 4 {
	case minecraftPre26Release:
		preKind := prerelease % 3
		preNumber := uint8(0)
		if preKind > 0 {
			preNumber = 1 + number%9
		}
		year := uint8(1 + int(first)%25)
		update := uint8(1 + int(second)%255)
		hotfix := third % 4
		text := fmt.Sprintf("%d.%d", year, update)
		if hotfix > 0 {
			text += fmt.Sprintf(".%d", hotfix)
		}
		if preKind > 0 {
			text += minecraftPrereleaseText(preKind, preNumber)
		}
		return minecraftFixture{
			text:   text,
			family: minecraftPre26Release,
			ordering: [5]uint8{
				year, update, hotfix,
				minecraftPrereleaseRank(preKind), preNumber,
			},
		}
	case minecraftPost26Release:
		preKind := prerelease % 3
		preNumber := uint8(0)
		hotfix := uint8(0)
		if preKind > 0 {
			preNumber = 1 + number%9
		} else {
			hotfix = third % 4
		}
		year := uint8(26 + int(first)%5)
		update := uint8(1 + int(second)%255)
		text := fmt.Sprintf("%d.%d", year, update)
		if preKind > 0 {
			text += minecraftPrereleaseText(preKind, preNumber)
		} else if hotfix > 0 {
			text += fmt.Sprintf(".%d", hotfix)
		}
		return minecraftFixture{
			text:   text,
			family: minecraftPost26Release,
			ordering: [5]uint8{
				year, update, hotfix,
				minecraftPrereleaseRank(preKind), preNumber,
			},
		}
	case minecraftPre26Snapshot:
		year := uint8(1 + int(first)%25)
		week := uint8(1 + int(second)%54)
		index := uint8('a' + int(third)%8)
		return minecraftFixture{
			text:     fmt.Sprintf("%dw%d%c", year, week, index),
			family:   minecraftPre26Snapshot,
			ordering: [5]uint8{year, week, index},
		}
	default:
		year := uint8(26 + int(first)%5)
		update := uint8(1 + int(second)%255)
		snapshot := uint8(1 + int(third)%9)
		return minecraftFixture{
			text:   fmt.Sprintf("%d.%d-snapshot-%d", year, update, snapshot),
			family: minecraftPost26Snapshot,
			ordering: [5]uint8{
				year, update, snapshot,
			},
		}
	}
}

func minecraftPrereleaseRank(kind uint8) uint8 {
	switch kind {
	case 1:
		return 0
	case 2:
		return 1
	default:
		return 2
	}
}

func minecraftPrereleaseText(kind, number uint8) string {
	if kind == 1 {
		return fmt.Sprintf("-pre%d", number)
	}
	return fmt.Sprintf("-rc%d", number)
}

// FuzzMinecraftVersionOrdering checks all four Minecraft version families
// against component-wise ordering. Seeds include cross-family pairs, which are
// incomparable and whose equality constraints only match after negation.
func FuzzMinecraftVersionOrdering(f *testing.F) {
	f.Add(uint8(0), uint8(1), uint8(1), uint8(20), uint8(0), uint8(26), uint8(1), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(0), uint8(0), uint8(1), uint8(20), uint8(0), uint8(1), uint8(20), uint8(1), uint8(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(0), uint8(0), uint8(1), uint8(20), uint8(0), uint8(1), uint8(20), uint8(0), uint8(1), uint8(2), uint8(1), uint8(1))
	f.Add(uint8(2), uint8(2), uint8(23), uint8(13), uint8(0), uint8(23), uint8(13), uint8(1), uint8(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(3), uint8(3), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(1), uint8(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(0), uint8(2), uint8(1), uint8(20), uint8(0), uint8(23), uint8(13), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(1), uint8(1), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(2), uint8(2), uint8(23), uint8(13), uint8(0), uint8(23), uint8(14), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(1), uint8(1), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(2), uint8(2), uint8(1), uint8(1))

	f.Fuzz(func(
		t *testing.T,
		kindLeft, kindRight,
		left0, left1, left2,
		right0, right1, right2,
		prereleaseLeft, prereleaseRight,
		numberLeft, numberRight uint8,
	) {
		left := newMinecraftFixture(
			kindLeft, left0, left1, left2,
			prereleaseLeft, numberLeft,
		)
		right := newMinecraftFixture(
			kindRight, right0, right1, right2,
			prereleaseRight, numberRight,
		)

		engine, err := version.NewEngine(version.DialectMinecraft)
		if err != nil {
			t.Fatalf("new Minecraft engine: %v", err)
		}
		leftVersion, err := engine.ParseVersion(version.Version(left.text))
		if err != nil {
			t.Fatalf("parse generated Minecraft version %q: %v", left.text, err)
		}
		rightVersion, err := engine.ParseVersion(version.Version(right.text))
		if err != nil {
			t.Fatalf("parse generated Minecraft version %q: %v", right.text, err)
		}

		wantComparable := left.family == right.family
		wantComparison := 0
		if wantComparable {
			wantComparison = normalizedSign(
				slices.Compare(left.ordering[:], right.ordering[:]),
			)
		}
		got, comparable := engine.Compare(leftVersion, rightVersion)
		if comparable != wantComparable {
			t.Fatalf(
				"Compare(%q, %q) comparable = %v, want %v",
				left.text, right.text, comparable, wantComparable,
			)
		}
		if comparable && normalizedSign(got) != wantComparison {
			t.Fatalf(
				"Compare(%q, %q) = %d, want sign %d",
				left.text, right.text, got, wantComparison,
			)
		}

		expr, err := engine.ParseConstraint(left.text)
		if err != nil {
			t.Fatalf("parse Minecraft constraint %q: %v", left.text, err)
		}
		matched := engine.Match(expr, rightVersion)
		wantMatched := wantComparable && wantComparison == 0
		if matched != wantMatched {
			t.Fatalf(
				"Minecraft exact constraint %q matches %q = %v, want %v",
				left.text, right.text, matched, wantMatched,
			)
		}
		if negated := engine.Match(expr.Negate(), rightVersion); negated == matched {
			t.Fatalf(
				"negated Minecraft constraint %q matches %q = %v, want %v",
				left.text, right.text, negated, !matched,
			)
		}
	})
}
