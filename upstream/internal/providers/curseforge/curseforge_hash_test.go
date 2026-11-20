package curseforge

import (
	"bytes"
	"crypto/sha1"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
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

func rawJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) {
	return 0, errors.New("body read failed")
}

func (failingBody) Close() error {
	return nil
}

func throttleHTTPResponse(retryAfter string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Retry-After": []string{retryAfter}},
		Body:       http.NoBody,
	}
}

func newHashTestProvider(t *testing.T, transport http.RoundTripper) *Provider {
	t.Helper()
	client := cache.New(
		t.TempDir(),
		cache.DefaultCacheConfig(),
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

func wireFingerprintFile(id, modID int32, fingerprint int64) fileResponse {
	return fileResponse{
		ID:              id,
		ModId:           modID,
		DisplayName:     "Beta 1.0",
		FileName:        "beta.jar",
		Hashes:          []fileHash{{Digest: "def", Algo: 1}},
		GameVersions:    []string{"1.20.1", "Fabric"},
		FileFingerprint: &fingerprint,
	}
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

func TestReleasesByHashUncorrelatedMatchIsNotCached(
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
	fingerprintRequests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.Path {
		case "/v1/fingerprints/432":
			fingerprintRequests++
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
			if fingerprintRequests > 1 {
				resp.Data.ExactMatches = []fingerprintMatch{{File: releaseFile(matchedFingerprint)}}
				return jsonResponse(t, resp), nil
			}
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
	artifacts := []upstream.ArtifactFingerprint{
		fixedFingerprint(matchedFingerprint),
		fixedFingerprint(malformedFingerprint),
		fixedFingerprint(missingOne),
		fixedFingerprint(missingTwo),
	}
	results := provider.ReleasesByHash(t.Context(), artifacts)

	if len(results) != 4 {
		t.Fatalf("results = %d, want 4", len(results))
	}
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

	fresh := provider.ReleasesByHash(t.Context(), artifacts)
	if fingerprintRequests != 2 || !fresh[0].Matched || fresh[0].Err != nil {
		t.Fatalf("second lookup = %+v, fingerprint requests = %d, want a fresh match", fresh, fingerprintRequests)
	}
	for i, result := range fresh[1:] {
		if result.Matched || result.Err != nil {
			t.Fatalf("second lookup, input %d = %+v, want a clean miss", i+1, result)
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
		return rawJSONResponse(
			`{"data":{"exactMatches":[]}}`,
		), nil
	})

	provider := newHashTestProvider(t, transport)
	artifacts := []upstream.ArtifactFingerprint{fixedFingerprint(5555)}
	results := provider.ReleasesByHash(t.Context(), artifacts)
	if results[0].Matched || results[0].Err != nil {
		t.Fatalf("empty response = %+v, want a clean miss", results[0])
	}

	cached := provider.ReleasesByHash(t.Context(), artifacts)
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 (empty matches stay cached)", requests)
	}
	if cached[0].Matched || cached[0].Err != nil {
		t.Fatalf("cached empty response = %+v, want a clean miss", cached[0])
	}
}

func TestReleasesByHashIncompleteEnvelopeIsNotCached(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing data", body: `{}`},
		{name: "missing exact matches", body: `{"data":{}}`},
		{name: "null exact matches", body: `{"data":{"exactMatches":null}}`},
		{name: "non-array exact matches", body: `{"data":{"exactMatches":{}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			requests := 0
			provider := newHashTestProvider(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				if request.URL.Path != "/v1/fingerprints/432" {
					t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
				}
				if requests == 1 {
					return rawJSONResponse(tc.body), nil
				}
				return rawJSONResponse(`{"data":{"exactMatches":[]}}`), nil
			}))
			artifacts := []upstream.ArtifactFingerprint{fixedFingerprint(5555)}
			first := provider.ReleasesByHash(t.Context(), artifacts)[0]
			if first.Matched || first.Err == nil {
				t.Fatalf("invalid envelope = %+v, want incomplete lookup", first)
			}
			second := provider.ReleasesByHash(t.Context(), artifacts)[0]
			if second.Matched || second.Err != nil || requests != 2 {
				t.Fatalf("lookup after malformed response = %+v, requests = %d, want a fresh miss", second, requests)
			}
		})
	}
}

func TestReleasesByHashOutOfRangeMatchesDoNotCorrelate(t *testing.T) {
	t.Parallel()
	const (
		zeroFingerprint = uint32(0)
		maxFingerprint  = uint32(math.MaxUint32)
	)

	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.Path {
		case "/v1/fingerprints/432":
			resp := fingerprintResponse{}
			resp.Data.ExactMatches = []fingerprintMatch{
				// Both wire values alias a requested fingerprint after uint32
				// narrowing: 1<<32 becomes 0 and -1 becomes MaxUint32.
				{File: wireFingerprintFile(300, 30, 1<<32)},
				{File: wireFingerprintFile(400, 40, -1)},
				// A present zero is a valid fingerprint.
				{File: releaseFile(zeroFingerprint)},
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
		fixedFingerprint(zeroFingerprint),
		fixedFingerprint(maxFingerprint),
	})

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (fingerprints and mods)", requests)
	}
	if results[0].Err != nil || !results[0].Matched {
		t.Fatalf("valid zero fingerprint = %+v, want a release", results[0])
	}
	if got := string(results[0].Release.Ref.Name); got != "alpha" {
		t.Fatalf("release name = %q, want alpha", got)
	}
	if got := string(results[0].Release.Ref.Version); got != "Alpha 1.0" {
		t.Fatalf("release version = %q, want Alpha 1.0", got)
	}
	if results[1].Matched || results[1].Err == nil ||
		!strings.Contains(results[1].Err.Error(), "cannot be correlated") {
		t.Fatalf(
			"input aliased by an out-of-range match = %+v, want an incomplete-lookup error",
			results[1],
		)
	}
}

func TestReleasesByHashUnrequestedMatchMarksUndeterminedInputsIncomplete(
	t *testing.T,
) {
	t.Parallel()
	const (
		matchedFingerprint     = uint32(7777)
		missingFingerprint     = uint32(6666)
		unrequestedFingerprint = uint32(8888)
	)

	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.Path {
		case "/v1/fingerprints/432":
			resp := fingerprintResponse{}
			resp.Data.ExactMatches = []fingerprintMatch{
				{File: releaseFile(matchedFingerprint)},
				// A real record for a fingerprint this batch did not ask for.
				{File: wireFingerprintFile(
					300,
					30,
					int64(unrequestedFingerprint),
				)},
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
		fixedFingerprint(missingFingerprint),
	})

	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (fingerprints and mods)", requests)
	}
	if results[0].Err != nil || !results[0].Matched {
		t.Fatalf("correlated match = %+v, want a release", results[0])
	}
	if results[1].Matched || results[1].Err == nil ||
		!strings.Contains(results[1].Err.Error(), "cannot be correlated") {
		t.Fatalf(
			"undetermined input = %+v, want an incomplete-lookup error",
			results[1],
		)
	}
}

func TestReleasesByHashRefetchRequiresCompleteAuthoritativeDetail(
	t *testing.T,
) {
	t.Parallel()
	const (
		incompleteFingerprint    = uint32(1111)
		validFingerprint         = uint32(2222)
		contradictoryFingerprint = uint32(3333)
		aliasedDetailFingerprint = uint32(0)
	)

	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		switch request.URL.Path {
		case "/v1/fingerprints/432":
			resp := fingerprintResponse{}
			resp.Data.ExactMatches = []fingerprintMatch{
				{File: fileResponse{
					ID:              100,
					ModId:           10,
					FileFingerprint: fingerprintPointer(incompleteFingerprint),
				}},
				{File: fileResponse{
					ID:              200,
					ModId:           20,
					FileFingerprint: fingerprintPointer(validFingerprint),
				}},
				{File: fileResponse{
					ID:              300,
					ModId:           30,
					FileFingerprint: fingerprintPointer(contradictoryFingerprint),
				}},
				{File: fileResponse{
					ID:              400,
					ModId:           40,
					FileFingerprint: fingerprintPointer(aliasedDetailFingerprint),
				}},
			}
			return jsonResponse(t, resp), nil
		case "/v1/mods/files":
			return jsonResponse(t, filesDataResponse{
				Data: []fileResponse{
					// Still incomplete, and its omitted detail fingerprint
					// leaves the independently validated inline identity intact.
					{
						ID:    100,
						ModId: 10,
					},
					wireFingerprintFile(200, 20, int64(validFingerprint)),
					// The authoritative detail contradicts the exact match.
					wireFingerprintFile(300, 30, 9999),
					// 1<<32 aliases the requested zero after uint32 narrowing.
					wireFingerprintFile(400, 40, 1<<32),
				},
			}), nil
		case "/v1/mods":
			return jsonResponse(t, modsDataResponse{
				Data: []modResponse{{ID: 20, Slug: "beta"}},
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
		fixedFingerprint(incompleteFingerprint),
		fixedFingerprint(validFingerprint),
		fixedFingerprint(contradictoryFingerprint),
		fixedFingerprint(aliasedDetailFingerprint),
	})

	if len(results) != 4 {
		t.Fatalf("results = %d, want 4", len(results))
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3 (fingerprints, files and mods)", requests)
	}
	if results[0].Matched || results[0].Err == nil ||
		!strings.Contains(results[0].Err.Error(), "incomplete release details") {
		t.Fatalf(
			"incomplete authoritative detail = %+v, want an incomplete-lookup error",
			results[0],
		)
	}
	if results[1].Err != nil || !results[1].Matched {
		t.Fatalf("valid authoritative detail = %+v, want a release", results[1])
	}
	if got := string(results[1].Release.Ref.Name); got != "beta" {
		t.Fatalf("release name = %q, want beta", got)
	}
	if results[2].Matched || results[2].Err == nil ||
		!strings.Contains(results[2].Err.Error(), "exact match reported") {
		t.Fatalf(
			"contradictory authoritative detail = %+v, want a fingerprint error",
			results[2],
		)
	}
	if results[3].Matched || results[3].Err == nil ||
		!strings.Contains(results[3].Err.Error(), "exact match reported") {
		t.Fatalf(
			"aliased authoritative detail = %+v, want a fingerprint error",
			results[3],
		)
	}
}

func TestReleasesByHashRefetchDoesNotCacheOmittedFiles(t *testing.T) {
	t.Parallel()
	firstFile := releaseFile(1111)
	secondFile := wireFingerprintFile(200, firstFile.ModId, 2222)
	requests := 0
	provider := newHashTestProvider(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v1/fingerprints/432":
			resp := fingerprintResponse{}
			for _, file := range []fileResponse{firstFile, secondFile} {
				file.FileName = "" // Require a separate file-details lookup.
				resp.Data.ExactMatches = append(resp.Data.ExactMatches, fingerprintMatch{File: file})
			}
			return jsonResponse(t, resp), nil
		case "/v1/mods/files":
			requests++
			files := []fileResponse{firstFile}
			if requests > 1 {
				files = append(files, secondFile)
			}
			return jsonResponse(t, filesDataResponse{Data: files}), nil
		case "/v1/mods":
			return jsonResponse(t, modsDataResponse{
				Data: []modResponse{{ID: firstFile.ModId, Slug: "alpha"}},
			}), nil
		default:
			return nil, fmt.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))

	artifacts := []upstream.ArtifactFingerprint{fixedFingerprint(1111), fixedFingerprint(2222)}
	results := provider.ReleasesByHash(t.Context(), artifacts)
	if !results[0].Matched || results[0].Err != nil {
		t.Fatalf("present file = %+v, want a release", results[0])
	}
	if results[1].Matched || results[1].Err == nil || !strings.Contains(results[1].Err.Error(), "exact file 200 is missing") {
		t.Fatalf("omitted file = %+v, want an incomplete lookup", results[1])
	}

	results = provider.ReleasesByHash(t.Context(), artifacts)
	if requests != 2 || !results[1].Matched || results[1].Err != nil {
		t.Fatalf("corrected file = %+v, file requests = %d, want a fresh match", results[1], requests)
	}
}

func TestReleasesByHashThrottleStopsSplitAndLaterBatches(t *testing.T) {
	t.Parallel()
	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Path != "/v1/fingerprints/432" {
			t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
		}
		return throttleHTTPResponse("60"), nil
	})

	provider := newHashTestProvider(t, transport)
	artifacts := make(
		[]upstream.ArtifactFingerprint,
		lookupBatchSize+1,
	)
	for i := range artifacts {
		artifacts[i] = fixedFingerprint(uint32(i + 1))
	}
	results := provider.ReleasesByHash(t.Context(), artifacts)

	if requests != 1 {
		t.Fatalf("requests = %d, want 1 (throttle must stop the lookup)", requests)
	}
	for i, result := range results {
		if result.Matched || result.Err == nil ||
			!strings.Contains(result.Err.Error(), "throttled") {
			t.Fatalf(
				"fingerprint %d = %+v, want a throttle error",
				i,
				result,
			)
		}
	}
}

func TestReleasesByHashProjectThrottlePreservesEarlierMatches(t *testing.T) {
	t.Parallel()
	projectRequests := 0
	provider := newHashTestProvider(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/v1/fingerprints/432":
			var payload fingerprintRequest
			if err := json.UnmarshalRead(request.Body, &payload); err != nil {
				return nil, err
			}
			var response fingerprintResponse
			for _, fingerprint := range payload.Fingerprints {
				id := int32(fingerprint)
				response.Data.ExactMatches = append(response.Data.ExactMatches, fingerprintMatch{
					File: wireFingerprintFile(id, id, int64(fingerprint)),
				})
			}
			return jsonResponse(t, response), nil
		case "/v1/mods":
			projectRequests++
			if projectRequests > 1 {
				return throttleHTTPResponse("60"), nil
			}
			var payload struct {
				ModIDs []int32 `json:"modIds"`
			}
			if err := json.UnmarshalRead(request.Body, &payload); err != nil {
				return nil, err
			}
			mods := make([]modResponse, 0, len(payload.ModIDs))
			for _, id := range payload.ModIDs {
				mods = append(mods, modResponse{ID: id, Slug: "beta"})
			}
			return jsonResponse(t, modsDataResponse{Data: mods}), nil
		default:
			return nil, fmt.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	artifacts := make([]upstream.ArtifactFingerprint, 2*lookupBatchSize+1)
	for i := range artifacts {
		artifacts[i] = fixedFingerprint(i + 1)
	}
	results := provider.ReleasesByHash(t.Context(), artifacts)
	if projectRequests != 2 {
		t.Fatalf("made %d project requests, want one success and one throttle", projectRequests)
	}
	for i, result := range results {
		if i < lookupBatchSize {
			if !result.Matched || result.Err != nil {
				t.Fatalf("input %d = %+v, want a release", i, result)
			}
			continue
		}
		if result.Matched || result.Err == nil || !strings.Contains(result.Err.Error(), "throttled") {
			t.Fatalf("input %d = %+v, want a throttle error", i, result)
		}
	}
}

func TestReleasesByHashThrottledBodyReadErrorStopsRetries(t *testing.T) {
	t.Parallel()
	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": []string{"60"}},
			Body:       failingBody{},
		}, nil
	})

	provider := newHashTestProvider(t, transport)
	results := provider.ReleasesByHash(
		t.Context(),
		[]upstream.ArtifactFingerprint{
			fixedFingerprint(1),
			fixedFingerprint(2),
		},
	)

	if requests != 1 {
		t.Fatalf(
			"requests = %d, want 1 (throttle status must survive a body read error)",
			requests,
		)
	}
	for i, result := range results {
		if result.Matched || result.Err == nil ||
			!strings.Contains(result.Err.Error(), "throttled") {
			t.Fatalf(
				"fingerprint %d = %+v, want a throttle error",
				i,
				result,
			)
		}
	}
}

func TestReleasesByHash404WithBodyReadErrorIsAMiss(t *testing.T) {
	t.Parallel()
	requests := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       failingBody{},
		}, nil
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
		t.Fatalf(
			"404 with a failed body read = %+v, want a miss",
			results[0],
		)
	}
}
