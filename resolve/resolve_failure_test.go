package resolve

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mclucy/lucy/catalog"
	serverpkg "github.com/mclucy/lucy/server"

	versionpkg "github.com/mclucy/lucy/version"
)

func failureTestKey(project string) projectKey {
	return projectKey{
		environment: serverpkg.EnvironmentServer,
		ecosystem:   catalog.EcosystemFabric,
		provider:    catalog.ProviderModrinth,
		project:     project,
	}
}

// A project whose ledger is dominated by nested rejections must not win a
// tie against a project whose rejections the failure report can show: build
// drops the nested groups, so their counts must not decide the summary.
func TestSummaryTieBreakIgnoresNestedRejections(t *testing.T) {
	ledger := newRejectionLedger()
	deep := failureTestKey("deep")
	shallow := failureTestKey("shallow")

	ledger.record(deep, "Deep", "1.8.0", "missing required module", RejectionModules)
	for i := range 8 {
		ledger.record(
			deep,
			"Deep",
			fmt.Sprintf("1.%d.0", i),
			"a required dependency could not be resolved",
			RejectionNested,
		)
	}
	ledger.markFailed(deep)

	ledger.record(shallow, "Shallow", "1.0.0", "requires a module the server cannot satisfy", RejectionModules)
	ledger.record(shallow, "Shallow", "1.1.0", "did not match 1.2.x", RejectionVersionMismatch)
	ledger.record(shallow, "Shallow", "1.2.0", "did not match 1.2.x", RejectionVersionMismatch)
	ledger.markFailed(shallow)

	failure := ledger.summary(versionpkg.Version("1.21.4"), errors.New("resolution failed"))
	if failure == nil {
		t.Fatal("summary returned no failure")
	}
	if failure.Project != "Shallow" {
		t.Fatalf(
			"summary picked %q; the tie must break on shown evidence, not nested counts",
			failure.Project,
		)
	}
}

// Projects with only nested rejections keep the existing comparison: the
// summary still names the project with the most rejections.
func TestSummaryNestedOnlyProjectsKeepTotalComparison(t *testing.T) {
	ledger := newRejectionLedger()
	few := failureTestKey("few")
	many := failureTestKey("many")

	ledger.record(few, "Few", "1.0.0", "a required dependency could not be resolved", RejectionNested)
	ledger.record(few, "Few", "1.1.0", "a required dependency could not be resolved", RejectionNested)
	ledger.markFailed(few)

	for i := range 5 {
		ledger.record(
			many,
			"Many",
			fmt.Sprintf("1.%d.0", i),
			"a required dependency could not be resolved",
			RejectionNested,
		)
	}
	ledger.markFailed(many)

	failure := ledger.summary(versionpkg.Version("1.21.4"), errors.New("resolution failed"))
	if failure == nil {
		t.Fatal("summary returned no failure")
	}
	if failure.Project != "Many" {
		t.Fatalf("summary picked %q; nested-only projects keep the total comparison", failure.Project)
	}
	for _, group := range failure.Groups {
		if group.Kind != RejectionNested {
			t.Fatalf("nested-only failure reported a %q group", group.Kind)
		}
	}
}

// Direct evidence still outranks any number of nested rejections.
func TestSummaryDirectEvidenceOutranksNestedRejections(t *testing.T) {
	ledger := newRejectionLedger()
	nestedOnly := failureTestKey("nested-only")
	direct := failureTestKey("direct")

	for i := range 9 {
		ledger.record(
			nestedOnly,
			"NestedOnly",
			fmt.Sprintf("1.%d.0", i),
			"a required dependency could not be resolved",
			RejectionNested,
		)
	}
	ledger.markFailed(nestedOnly)

	ledger.record(direct, "Direct", "1.0.0", "did not match 1.2.x", RejectionVersionMismatch)
	ledger.markFailed(direct)

	failure := ledger.summary(versionpkg.Version("1.21.4"), errors.New("resolution failed"))
	if failure == nil {
		t.Fatal("summary returned no failure")
	}
	if failure.Project != "Direct" {
		t.Fatalf("summary picked %q; direct evidence outranks nested rejections", failure.Project)
	}
}
