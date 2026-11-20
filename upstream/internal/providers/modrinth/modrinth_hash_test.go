package modrinth

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/mclucy/lucy/upstream"
)

type hashFingerprint [sha1.Size]byte

func (f hashFingerprint) SHA1() ([sha1.Size]byte, error) {
	return [sha1.Size]byte(f), nil
}

func (f hashFingerprint) MurmurHash() (uint32, error) {
	return 0, nil
}

func (f hashFingerprint) hex() string {
	digest := [sha1.Size]byte(f)
	return hex.EncodeToString(digest[:])
}

func TestReleasesByHashIncompleteMatchIsAnError(t *testing.T) {
	t.Parallel()
	noProject := hashFingerprint{0x01}
	noFiles := hashFingerprint{0x02}
	omitted := hashFingerprint{0x03}

	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodPost &&
			request.URL.Path == "/v2/version_files":
			return jsonResponse(t, map[string]versionResponse{
				noProject.hex(): {
					ID:    "version-no-project",
					Files: []fileResponse{{Filename: "orphan.jar"}},
				},
				noFiles.hex(): {
					ID:        "version-no-files",
					ProjectId: "project-no-files",
				},
			}), nil
		case request.Method == http.MethodGet &&
			request.URL.Path == "/v2/projects":
			return jsonResponse(t, []projectResponse{{
				ID:   "project-no-files",
				Slug: "no-files",
			}}), nil
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		}
	})

	provider := newModrinthTestProvider(t, transport)
	results := provider.ReleasesByHash(
		t.Context(),
		[]upstream.ArtifactFingerprint{noProject, noFiles, omitted},
	)

	if results[0].Matched ||
		!errors.Is(results[0].Err, ErrInvalidAPIResponse) {
		t.Fatalf(
			"match without a project = %+v, want an invalid-response error",
			results[0],
		)
	}
	if !strings.Contains(results[0].Err.Error(), noProject.hex()) {
		t.Errorf(
			"project error %q does not name hash %s",
			results[0].Err,
			noProject.hex(),
		)
	}
	if results[1].Matched ||
		!errors.Is(results[1].Err, ErrInvalidAPIResponse) {
		t.Fatalf(
			"match without files = %+v, want an invalid-response error",
			results[1],
		)
	}
	if !strings.Contains(results[1].Err.Error(), noFiles.hex()) {
		t.Errorf(
			"files error %q does not name hash %s",
			results[1].Err,
			noFiles.hex(),
		)
	}
	if results[2].Matched || results[2].Err != nil {
		t.Fatalf("omitted digest = %+v, want a clean miss", results[2])
	}
}
