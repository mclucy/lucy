package resolve

import (
	"testing"
	"time"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"

	versionpkg "github.com/mclucy/lucy/version"
)

func testCandidate(
	version, releaseID, fileID, filename string,
	published time.Time,
	channel upstream.ReleaseChannel,
	primary bool,
) upstream.Candidate {
	return upstream.Candidate{
		Artifact: catalog.Artifact{
			Version:   versionpkg.Version(version),
			ReleaseID: releaseID,
			FileID:    fileID,
			Filename:  filename,
		},
		Channel:     channel,
		PublishedAt: published,
		PrimaryFile: primary,
	}
}

func testRequest(selector versionpkg.Version, pattern string) projectRequest {
	return projectRequest{
		key: projectKey{
			environment: serverpkg.EnvironmentServer,
			ecosystem:   catalog.EcosystemFabric,
			provider:    catalog.ProviderModrinth,
			project:     "demo",
		},
		display: "demo",
		origin:  originManifest,
		requirements: []lockfile.RequirementBinding{{
			Reference:   "modrinth:demo",
			Selector:    selector,
			FilePattern: pattern,
			Enabled:     true,
		}},
	}
}

func TestDecideCandidate(t *testing.T) {
	release := testCandidate("1.0.0", "r1", "f1", "demo.jar", time.Unix(1, 0), upstream.ReleaseChannelRelease, true)
	beta := testCandidate("1.1.0", "r2", "f2", "demo-1.1.0.jar", time.Unix(2, 0), upstream.ReleaseChannelBeta, true)
	unprimary := testCandidate("1.0.0", "r1", "f1", "demo-sources.jar", time.Unix(1, 0), upstream.ReleaseChannelRelease, false)

	tests := []struct {
		name       string
		candidate  upstream.Candidate
		request    projectRequest
		want       bool
		wantKind   RejectionKind
		wantDetail string
		wantErr    bool
	}{
		{
			name:      "stable accepts a release",
			candidate: release,
			request:   testRequest(versionpkg.Stable, ""),
			want:      true,
		},
		{
			name:       "stable rejects a prerelease",
			candidate:  beta,
			request:    testRequest(versionpkg.Stable, ""),
			wantKind:   RejectionChannel,
			wantDetail: "requested a release build",
		},
		{
			name:      "any accepts a prerelease",
			candidate: beta,
			request:   testRequest(versionpkg.Any, ""),
			want:      true,
		},
		{
			name:      "beta accepts a prerelease",
			candidate: beta,
			request:   testRequest(versionpkg.Beta, ""),
			want:      true,
		},
		{
			name:      "exact version matches the version",
			candidate: release,
			request:   testRequest("1.0.0", ""),
			want:      true,
		},
		{
			name:       "exact version rejects another version",
			candidate:  beta,
			request:    testRequest("1.0.0", ""),
			wantKind:   RejectionVersionMismatch,
			wantDetail: "1.0.0",
		},
		{
			name:      "exact version matches a release id",
			candidate: release,
			request:   testRequest("r1", ""),
			want:      true,
		},
		{
			name:      "exact version matches a file id",
			candidate: release,
			request:   testRequest("f1", ""),
			want:      true,
		},
		{
			name:      "exact version matches a filename",
			candidate: release,
			request:   testRequest("demo.jar", ""),
			want:      true,
		},
		{
			name:      "file pattern matches",
			candidate: beta,
			request:   testRequest(versionpkg.Any, "demo-*.jar"),
			want:      true,
		},
		{
			name:       "file pattern rejects",
			candidate:  release,
			request:    testRequest(versionpkg.Any, "demo-*.jar"),
			wantKind:   RejectionFilePattern,
			wantDetail: "demo-*.jar",
		},
		{
			name:       "constrained request rejects a non-primary file",
			candidate:  unprimary,
			request:    testRequest(versionpkg.Any, ""),
			wantKind:   RejectionPrimaryFile,
			wantDetail: "",
		},
		{
			name:       "dependency request rejects a non-primary file",
			candidate:  unprimary,
			request:    projectRequest{origin: originDependency},
			wantKind:   RejectionPrimaryFile,
			wantDetail: "",
		},
		{
			name:      "release hint does not reject",
			candidate: release,
			request:   projectRequest{origin: originDependency, releaseHints: []releaseHint{{releaseID: "r9"}}},
			want:      true,
		},
		{
			name:      "disabled requirement does not constrain",
			candidate: release,
			request: projectRequest{
				origin: originManifest,
				requirements: []lockfile.RequirementBinding{{
					Reference: "modrinth:demo",
					Selector:  "9.9.9",
					Enabled:   false,
				}},
			},
			want: true,
		},
		{
			name:       "first rejection wins",
			candidate:  beta,
			request:    projectRequest{origin: originManifest, requirements: testRequest(versionpkg.Stable, "").requirements},
			wantKind:   RejectionChannel,
			wantDetail: "requested a release build",
		},
		{
			name:      "invalid file pattern is an error",
			candidate: release,
			request:   testRequest(versionpkg.Any, "["),
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decideCandidate(tt.candidate, tt.request)
			if tt.wantErr {
				if err == nil {
					t.Fatal("decideCandidate() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("decideCandidate() error = %v", err)
			}
			if got.eligible != tt.want {
				t.Fatalf("decideCandidate() eligible = %v, want %v", got.eligible, tt.want)
			}
			if got.kind != tt.wantKind {
				t.Errorf("decideCandidate() kind = %q, want %q", got.kind, tt.wantKind)
			}
			if got.detail != tt.wantDetail {
				t.Errorf("decideCandidate() detail = %q, want %q", got.detail, tt.wantDetail)
			}
		})
	}
}

func TestOrderCandidatesNewestFirst(t *testing.T) {
	oldest := testCandidate("1.0.0", "r1", "f1", "demo-1.0.0.jar", time.Unix(1, 0), upstream.ReleaseChannelBeta, true)
	middle := testCandidate("1.1.0", "r2", "f2", "demo-1.1.0.jar", time.Unix(2, 0), upstream.ReleaseChannelBeta, true)
	newest := testCandidate("1.2.0", "r3", "f3", "demo-1.2.0.jar", time.Unix(3, 0), upstream.ReleaseChannelBeta, true)

	got := orderCandidates(
		[]upstream.Candidate{middle, oldest, newest},
		nil,
		testRequest(versionpkg.Beta, ""),
	)
	want := []string{"r3", "r2", "r1"}
	for i, candidate := range got {
		if candidate.ReleaseID != want[i] {
			t.Fatalf("orderCandidates()[%d] = %q, want %q", i, candidate.ReleaseID, want[i])
		}
	}
}

func TestOrderCandidatesKeepsLockedBuildFirst(t *testing.T) {
	locked := testCandidate("1.0.0", "r1", "f1", "demo-1.0.0.jar", time.Unix(1, 0), upstream.ReleaseChannelBeta, true)
	newer := testCandidate("1.1.0", "r2", "f2", "demo-1.1.0.jar", time.Unix(2, 0), upstream.ReleaseChannelBeta, true)
	previous := []lockfile.Package{{
		Environment: serverpkg.EnvironmentServer,
		Ecosystem:   catalog.EcosystemFabric,
		Artifact: catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: "demo",
			ReleaseID: "r1",
			FileID:    "f1",
		},
	}}

	got := orderCandidates(
		[]upstream.Candidate{newer, locked},
		previous,
		testRequest(versionpkg.Beta, ""),
	)
	if got[0].ReleaseID != "r1" {
		t.Fatalf("orderCandidates()[0] = %q, want the locked build", got[0].ReleaseID)
	}
}

func TestPreferReleaseBuildsForAny(t *testing.T) {
	beta := testCandidate("1.1.0", "r2", "f2", "demo-1.1.0.jar", time.Unix(2, 0), upstream.ReleaseChannelBeta, true)
	release := testCandidate("1.0.0", "r1", "f1", "demo-1.0.0.jar", time.Unix(1, 0), upstream.ReleaseChannelRelease, true)

	got := preferReleaseBuilds(
		[]upstream.Candidate{beta, release},
		testRequest(versionpkg.Any, "").requirements,
	)
	if got[0].ReleaseID != "r1" {
		t.Fatalf("preferReleaseBuilds()[0] = %q, want the release build", got[0].ReleaseID)
	}
}

// TestOrderCandidatesLockedBetaStillYieldsToRelease documents the current
// behavior: the locked build moves first, then the `any` release preference
// can move an older release ahead of it. The upgrade path must not inherit
// this as an accidental downgrade.
func TestOrderCandidatesLockedBetaStillYieldsToRelease(t *testing.T) {
	beta := testCandidate("1.2.0", "r2", "f2", "demo-1.2.0-beta.jar", time.Unix(2, 0), upstream.ReleaseChannelBeta, true)
	release := testCandidate("1.0.0", "r1", "f1", "demo-1.0.0.jar", time.Unix(1, 0), upstream.ReleaseChannelRelease, true)
	previous := []lockfile.Package{{
		Environment: serverpkg.EnvironmentServer,
		Ecosystem:   catalog.EcosystemFabric,
		Artifact: catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: "demo",
			ReleaseID: "r2",
			FileID:    "f2",
		},
	}}

	request := testRequest(versionpkg.Any, "")
	got := preferReleaseBuilds(
		orderCandidates([]upstream.Candidate{beta, release}, previous, request),
		request.requirements,
	)
	if got[0].ReleaseID != "r1" {
		t.Fatalf("ordered candidates [0] = %q, want the older release under current behavior", got[0].ReleaseID)
	}
}

func TestPreferReleaseHints(t *testing.T) {
	older := testCandidate("1.0.0", "r1", "f1", "demo-1.0.0.jar", time.Unix(1, 0), upstream.ReleaseChannelRelease, true)
	newer := testCandidate("1.1.0", "r2", "f2", "demo-1.1.0.jar", time.Unix(2, 0), upstream.ReleaseChannelRelease, true)

	got := preferReleaseHints(
		[]upstream.Candidate{newer, older},
		[]releaseHint{{releaseID: "r1"}},
	)
	if got[0].ReleaseID != "r1" {
		t.Fatalf("preferReleaseHints()[0] = %q, want the hinted release", got[0].ReleaseID)
	}
}

func TestMetadataConflict(t *testing.T) {
	request := testRequest(versionpkg.Any, "")
	candidate := testCandidate("2.0.0", "r2", "f2", "demo-2.0.0.jar", time.Unix(2, 0), upstream.ReleaseChannelRelease, true)
	candidate.Provider = catalog.ProviderModrinth
	candidate.ProjectID = "demo"
	candidate.Dependencies = []upstream.ProjectDependency{{
		Provider:  catalog.ProviderModrinth,
		ProjectID: "target",
		ReleaseID: "r1",
		Kind:      catalog.DependencyKindIncompatible,
	}}
	selected := lockfile.Package{
		Environment: serverpkg.EnvironmentServer,
		Ecosystem:   catalog.EcosystemFabric,
		Artifact: catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: "target",
			ReleaseID: "r1",
			Filename:  "target.jar",
		},
	}
	chosen := map[projectKey]lockfile.Package{{
		environment: serverpkg.EnvironmentServer,
		ecosystem:   catalog.EcosystemFabric,
		provider:    catalog.ProviderModrinth,
		project:     "target",
	}: selected}

	if detail, conflicting := metadataConflict(candidate, request, chosen, nil); !conflicting || detail != "modrinth:target@r1" {
		t.Fatalf("metadataConflict() = %q, %v, want the selected release conflict", detail, conflicting)
	}

	selected.ReleaseID = "r0"
	chosen[projectKey{
		environment: serverpkg.EnvironmentServer,
		ecosystem:   catalog.EcosystemFabric,
		provider:    catalog.ProviderModrinth,
		project:     "target",
	}] = selected
	if _, conflicting := metadataConflict(candidate, request, chosen, nil); conflicting {
		t.Fatal("metadataConflict() reported a conflict against a different release")
	}

	chosen = map[projectKey]lockfile.Package{}
	conflicts := map[projectKey][]upstream.ProjectDependency{{
		environment: serverpkg.EnvironmentServer,
		ecosystem:   catalog.EcosystemFabric,
		provider:    catalog.ProviderModrinth,
		project:     "source",
	}: {{
		Provider:  catalog.ProviderModrinth,
		ProjectID: "demo",
		ReleaseID: "r2",
		Kind:      catalog.DependencyKindConflict,
	}}}
	if detail, conflicting := metadataConflict(candidate, request, chosen, conflicts); !conflicting || detail != "modrinth:demo@r2" {
		t.Fatalf("metadataConflict() reverse = %q, %v, want the selected package conflict", detail, conflicting)
	}
}
