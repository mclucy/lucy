package curseforge

import (
	"bytes"
	"crypto/sha1"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/upstream"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type fixedFingerprint uint32

func (f fixedFingerprint) SHA1() ([sha1.Size]byte, error) {
	return [sha1.Size]byte{}, nil
}

func (f fixedFingerprint) MurmurHash() (uint32, error) {
	return uint32(f), nil
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

func newHashTestProvider(t *testing.T, transport http.RoundTripper) *Provider {
	t.Helper()
	client := cache.New(
		t.TempDir(),
		cache.Config{},
		&http.Client{Transport: transport},
		nil,
	)
	t.Cleanup(func() { _ = client.Close() })
	return New(client, nil, "test-key")
}

func fingerprintPointer(value uint32) *int64 {
	fingerprint := int64(value)
	return &fingerprint
}

func releaseFile(fingerprint uint32) fileResponse {
	return fileResponse{
		ID:              100,
		ModId:           10,
		DisplayName:     "Alpha 1.0",
		FileName:        "alpha.jar",
		Hashes:          []fileHash{{Digest: "abc", Algo: 1}},
		GameVersions:    []string{"1.20.1", "Forge"},
		FileFingerprint: fingerprintPointer(fingerprint),
	}
}

func TestReleasesByHashUncorrelatedMatchMarksUndeterminedInputsIncomplete(
	t *testing.T,
) {
	t.Parallel()
	const (
		matchedFingerprint   = uint32(1111)
		malformedFingerprint = uint32(2222)
		missingOne           = uint32(3333)
		missingTwo           = uint32(4444)
	)

	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.Path {
		case "/v1/fingerprints/432":
			payload, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read fingerprint request: %v", err)
			}
			var decoded fingerprintRequest
			if err := json.Unmarshal(payload, &decoded); err != nil {
				t.Errorf("decode fingerprint request: %v", err)
			}
			if len(decoded.Fingerprints) != 4 {
				t.Errorf(
					"fingerprint request carried %d inputs, want 4",
					len(decoded.Fingerprints),
				)
			}
			resp := fingerprintResponse{}
			resp.Data.ExactMatches = []fingerprintMatch{
				{File: releaseFile(matchedFingerprint)},
				// Correlated by fingerprint but missing file identity.
				{File: fileResponse{
					FileFingerprint: fingerprintPointer(malformedFingerprint),
				}},
				// A real record the batch cannot attribute to any input.
				{File: fileResponse{
					ID:          300,
					ModId:       30,
					DisplayName: "Gamma 1.0",
					FileName:    "gamma.jar",
				}},
			}
			return jsonResponse(t, resp), nil
		case "/v1/mods":
			return jsonResponse(t, modsDataResponse{
				Data: []modResponse{{ID: 10, Slug: "alpha"}},
			}), nil
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		}
	})

	provider := newHashTestProvider(t, transport)
	results := provider.ReleasesByHash(t.Context(), []upstream.ArtifactFingerprint{
		fixedFingerprint(matchedFingerprint),
		fixedFingerprint(malformedFingerprint),
		fixedFingerprint(missingOne),
		fixedFingerprint(missingTwo),
	})

	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (fingerprints and mods)", requests)
	}
	if results[0].Err != nil || !results[0].Matched {
		t.Fatalf("correlated match = %+v, want a release", results[0])
	}
	if got := string(results[0].Release.Ref.Name); got != "alpha" {
		t.Fatalf("release name = %q, want alpha", got)
	}
	if results[1].Matched || results[1].Err == nil ||
		!strings.Contains(results[1].Err.Error(), "missing file identity") {
		t.Fatalf("malformed match = %+v, want a missing-identity error", results[1])
	}
	for i := 2; i < len(results); i++ {
		if results[i].Matched || results[i].Err == nil ||
			!strings.Contains(results[i].Err.Error(), "cannot be correlated") {
			t.Fatalf(
				"undetermined input %d = %+v, want an incomplete-lookup error",
				i-2,
				results[i],
			)
		}
	}
}

func TestReleasesByHashEmptyResponseIsAMiss(t *testing.T) {
	t.Parallel()
	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Path != "/v1/fingerprints/432" {
			t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		return jsonResponse(t, fingerprintResponse{}), nil
	})

	provider := newHashTestProvider(t, transport)
	results := provider.ReleasesByHash(
		t.Context(),
		[]upstream.ArtifactFingerprint{fixedFingerprint(5555)},
	)

	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if results[0].Matched || results[0].Err != nil {
		t.Fatalf("empty response = %+v, want a clean miss", results[0])
	}
}
