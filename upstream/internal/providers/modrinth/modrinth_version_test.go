package modrinth

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func jsonResponse(t *testing.T, body any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(encoded)),
	}
}

func newModrinthTestProvider(t *testing.T, transport http.RoundTripper) *Provider {
	t.Helper()
	client := cache.New(
		t.TempDir(),
		cache.Config{},
		&http.Client{Transport: transport},
		nil,
	)
	t.Cleanup(func() { _ = client.Close() })
	return New(client, nil)
}

func TestPinnedVersionLookupRetriesFailedBatch(t *testing.T) {
	t.Parallel()
	fallbackRequests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v2/versions":
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       http.NoBody,
			}, nil
		case "/v2/version/pinned-version":
			fallbackRequests++
			return jsonResponse(t, versionResponse{
				ID:        "pinned-version",
				ProjectId: "pinned-project",
			}), nil
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		}
	})

	provider := newModrinthTestProvider(t, transport)
	root := &versionResponse{
		ID: "root-version",
		Dependencies: []dependenciesResponse{
			{VersionId: "pinned-version", DependencyType: "required"},
			{VersionId: "pinned-version", DependencyType: "required"},
		},
	}
	lookup := provider.pinnedVersionLookup(t.Context(), []*versionResponse{root})
	if lookup == nil {
		t.Fatal("expected a dependency lookup")
	}

	candidate, err := provider.candidateFromVersion(
		t.Context(),
		root,
		fileResponse{},
		true,
		true,
		lookup,
	)
	if err != nil {
		t.Fatalf("candidate from version: %v", err)
	}
	if candidate.DependenciesUnknown {
		t.Fatal("dependency metadata marked unknown after a successful fallback")
	}
	if len(candidate.Dependencies) != 2 {
		t.Fatalf("dependencies = %d, want 2", len(candidate.Dependencies))
	}
	for _, dependency := range candidate.Dependencies {
		if dependency.ProjectID != "pinned-project" {
			t.Errorf("dependency project = %q, want pinned-project", dependency.ProjectID)
		}
		if dependency.ReleaseID != "pinned-version" {
			t.Errorf("dependency release = %q, want pinned-version", dependency.ReleaseID)
		}
		if dependency.Kind != catalog.DependencyKindRequired {
			t.Errorf("dependency kind = %q, want required", dependency.Kind)
		}
	}
	if fallbackRequests != 1 {
		t.Fatalf("single-version requests = %d, want 1", fallbackRequests)
	}
}

func TestPinnedVersionLookupKeepsBulkResultsAndOmitsMissing(t *testing.T) {
	t.Parallel()
	singleRequests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Path == "/v2/versions":
			return jsonResponse(t, []versionResponse{{
				ID:        "bulk-version",
				ProjectId: "bulk-project",
			}}), nil
		case strings.HasPrefix(request.URL.Path, "/v2/version/"):
			singleRequests++
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		}
	})

	provider := newModrinthTestProvider(t, transport)
	lookup := provider.pinnedVersionLookup(t.Context(), []*versionResponse{{
		ID: "root-version",
		Dependencies: []dependenciesResponse{
			{VersionId: "bulk-version", DependencyType: "required"},
			{VersionId: "omitted-version", DependencyType: "required"},
		},
	}})
	if lookup == nil {
		t.Fatal("expected a dependency lookup")
	}

	bulk, err := lookup(t.Context(), "bulk-version")
	if err != nil {
		t.Fatalf("bulk version: %v", err)
	}
	if bulk.ProjectId != "bulk-project" {
		t.Fatalf("bulk project = %q, want bulk-project", bulk.ProjectId)
	}

	_, err = lookup(t.Context(), "omitted-version")
	if !errors.Is(err, ErrNoVersion) {
		t.Fatalf("omitted version error = %v, want ErrNoVersion", err)
	}
	if singleRequests != 0 {
		t.Fatalf("single-version requests = %d, want 0", singleRequests)
	}
}

func TestPinnedVersionLookupKeepsBothCausesWhenFallbackFails(t *testing.T) {
	t.Parallel()
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v2/versions":
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       http.NoBody,
			}, nil
		case "/v2/version/pinned-version":
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		}
	})

	provider := newModrinthTestProvider(t, transport)
	lookup := provider.pinnedVersionLookup(t.Context(), []*versionResponse{{
		ID: "root-version",
		Dependencies: []dependenciesResponse{
			{VersionId: "pinned-version", DependencyType: "required"},
		},
	}})
	if lookup == nil {
		t.Fatal("expected a dependency lookup")
	}

	_, err := lookup(t.Context(), "pinned-version")
	if !errors.Is(err, ErrInvalidAPIResponse) {
		t.Errorf("fallback failure = %v, want the batch cause", err)
	}
	if !errors.Is(err, ErrNoVersion) {
		t.Errorf("fallback failure = %v, want the fallback cause", err)
	}

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = lookup(canceled, "pinned-version")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled lookup error = %v, want context canceled", err)
	}
}

func TestPinnedVersionLookupFallsBackWhenBulkVersionHasNoProject(t *testing.T) {
	t.Parallel()
	fallbackRequests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v2/versions":
			return jsonResponse(t, []versionResponse{{
				ID: "pinned-version",
			}}), nil
		case "/v2/version/pinned-version":
			fallbackRequests++
			return jsonResponse(t, versionResponse{
				ID:        "pinned-version",
				ProjectId: "fallback-project",
			}), nil
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		}
	})

	provider := newModrinthTestProvider(t, transport)
	lookup := provider.pinnedVersionLookup(t.Context(), []*versionResponse{{
		ID: "root-version",
		Dependencies: []dependenciesResponse{
			{VersionId: "pinned-version", DependencyType: "required"},
			{VersionId: "pinned-version", DependencyType: "required"},
		},
	}})
	if lookup == nil {
		t.Fatal("expected a dependency lookup")
	}

	for range 2 {
		pinned, err := lookup(t.Context(), "pinned-version")
		if err != nil {
			t.Fatalf("project-less bulk version: %v", err)
		}
		if pinned.ProjectId != "fallback-project" {
			t.Fatalf(
				"pinned project = %q, want fallback-project",
				pinned.ProjectId,
			)
		}
	}
	if fallbackRequests != 1 {
		t.Fatalf("single-version requests = %d, want 1", fallbackRequests)
	}
}

func TestPinnedVersionLookupRejectsProjectlessFallback(t *testing.T) {
	t.Parallel()
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v2/versions":
			return jsonResponse(t, []versionResponse{{
				ID: "pinned-version",
			}}), nil
		case "/v2/version/pinned-version":
			return jsonResponse(t, versionResponse{
				ID: "pinned-version",
			}), nil
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		}
	})

	provider := newModrinthTestProvider(t, transport)
	lookup := provider.pinnedVersionLookup(t.Context(), []*versionResponse{{
		ID: "root-version",
		Dependencies: []dependenciesResponse{
			{VersionId: "pinned-version", DependencyType: "required"},
		},
	}})
	if lookup == nil {
		t.Fatal("expected a dependency lookup")
	}

	pinned, err := lookup(t.Context(), "pinned-version")
	if pinned != nil {
		t.Fatalf("project-less fallback returned %+v", pinned)
	}
	if !errors.Is(err, ErrInvalidAPIResponse) {
		t.Fatalf(
			"project-less fallback error = %v, want invalid response",
			err,
		)
	}
}

func TestResolveVersionByIdRejectsProjectlessVersion(t *testing.T) {
	t.Parallel()
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v2/version/standalone-version" {
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
		}
		return jsonResponse(t, versionResponse{
			ID: "standalone-version",
		}), nil
	})

	provider := newModrinthTestProvider(t, transport)
	pinned, err := provider.resolveVersionById(
		t.Context(),
		nil,
		"standalone-version",
	)
	if pinned != nil {
		t.Fatalf("project-less version returned %+v", pinned)
	}
	if !errors.Is(err, ErrInvalidAPIResponse) {
		t.Fatalf("project-less version error = %v, want invalid response", err)
	}
}

func TestPinnedVersionLookupKeepsBothCausesForProjectlessFallback(t *testing.T) {
	t.Parallel()
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v2/versions":
			return jsonResponse(t, []versionResponse{{
				ID: "pinned-version",
			}}), nil
		case "/v2/version/pinned-version":
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		}
	})

	provider := newModrinthTestProvider(t, transport)
	lookup := provider.pinnedVersionLookup(t.Context(), []*versionResponse{{
		ID: "root-version",
		Dependencies: []dependenciesResponse{
			{VersionId: "pinned-version", DependencyType: "required"},
		},
	}})
	if lookup == nil {
		t.Fatal("expected a dependency lookup")
	}

	_, err := lookup(t.Context(), "pinned-version")
	if !errors.Is(err, ErrInvalidAPIResponse) {
		t.Errorf("fallback failure = %v, want the malformed bulk cause", err)
	}
	if !errors.Is(err, ErrNoVersion) {
		t.Errorf("fallback failure = %v, want the fallback cause", err)
	}
}
