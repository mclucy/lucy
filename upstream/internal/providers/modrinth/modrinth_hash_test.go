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

func fileWithSha1(id, filename, sha1 string, primary bool) fileResponse {
	var file fileResponse
	file.ID = id
	file.Filename = filename
	file.Hashes.Sha1 = sha1
	file.IsPrimary = primary
	return file
}

func TestReleaseByHashMatchesExactSecondaryFile(t *testing.T) {
	t.Parallel()
	fingerprint := hashFingerprint{0x11}
	hash := fingerprint.hex()
	projectRequests := 0

	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == "/v2/version_file/"+hash:
			return jsonResponse(t, versionResponse{
				ID:            "version-secondary",
				ProjectId:     "project-secondary",
				VersionNumber: "1.2.3",
				Files: []fileResponse{
					fileWithSha1(
						"primary-file",
						"primary.jar",
						strings.Repeat("0", 40),
						true,
					),
					fileWithSha1(
						"secondary-file",
						"secondary.jar",
						strings.ToUpper(hash),
						false,
					),
				},
			}), nil
		case request.Method == http.MethodGet &&
			request.URL.Path == "/v2/project/project-secondary":
			projectRequests++
			return jsonResponse(t, projectResponse{
				ID:   "project-secondary",
				Slug: "secondary",
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
	release, matched, err := provider.ReleaseByHash(t.Context(), fingerprint)
	if err != nil {
		t.Fatalf("release by hash: %v", err)
	}
	if !matched {
		t.Fatal("expected the digest to match a release")
	}
	if release.Candidate.Filename != "secondary.jar" {
		t.Errorf(
			"matched filename = %q, want the file carrying the digest",
			release.Candidate.Filename,
		)
	}
	if release.Candidate.FileID != "secondary-file" {
		t.Errorf(
			"matched file id = %q, want secondary-file",
			release.Candidate.FileID,
		)
	}
	if release.Candidate.ProjectID != "project-secondary" {
		t.Errorf(
			"candidate project = %q, want project-secondary",
			release.Candidate.ProjectID,
		)
	}
	if string(release.Ref.Name) != "secondary" {
		t.Errorf("release name = %q, want secondary", release.Ref.Name)
	}
	if projectRequests != 1 {
		t.Fatalf("project requests = %d, want 1", projectRequests)
	}
}

func TestReleaseByHashIncompleteMatchIsAnError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		projectID string
		files     func(hash string) []fileResponse
	}{
		{
			name: "no project",
			files: func(hash string) []fileResponse {
				return []fileResponse{
					fileWithSha1("file", "file.jar", hash, false),
				}
			},
		},
		{
			name:      "no files",
			projectID: "project-no-files",
			files: func(string) []fileResponse {
				return nil
			},
		},
		{
			name:      "mismatched primary",
			projectID: "project-mismatch",
			files: func(string) []fileResponse {
				return []fileResponse{
					fileWithSha1(
						"file",
						"file.jar",
						strings.Repeat("0", 40),
						true,
					),
				}
			},
		},
		{
			name:      "missing file hash",
			projectID: "project-missing-hash",
			files: func(string) []fileResponse {
				return []fileResponse{
					fileWithSha1("file", "file.jar", "", true),
				}
			},
		},
	}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fingerprint := hashFingerprint{byte(0x40 + index)}
			hash := fingerprint.hex()
			projectRequests := 0

			transport := roundTripFunc(func(
				request *http.Request,
			) (*http.Response, error) {
				switch {
				case request.Method == http.MethodGet &&
					request.URL.Path == "/v2/version_file/"+hash:
					return jsonResponse(t, versionResponse{
						ID:        "version-incomplete",
						ProjectId: testCase.projectID,
						Files:     testCase.files(hash),
					}), nil
				case strings.HasPrefix(request.URL.Path, "/v2/project/"):
					projectRequests++
					return jsonResponse(t, projectResponse{
						ID:   testCase.projectID,
						Slug: "incomplete",
					}), nil
				default:
					t.Errorf(
						"unexpected request %s %s",
						request.Method,
						request.URL,
					)
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       http.NoBody,
					}, nil
				}
			})

			provider := newModrinthTestProvider(t, transport)
			_, matched, err := provider.ReleaseByHash(
				t.Context(),
				fingerprint,
			)
			if matched {
				t.Fatal("incomplete match reported as a release")
			}
			if !errors.Is(err, ErrInvalidAPIResponse) {
				t.Fatalf(
					"incomplete match error = %v, want invalid response",
					err,
				)
			}
			if !strings.Contains(err.Error(), hash) {
				t.Errorf("error %q does not name hash %s", err, hash)
			}
			if projectRequests != 0 {
				t.Fatalf(
					"project requests = %d, want 0 for an unmatched digest",
					projectRequests,
				)
			}
		})
	}
}

func TestReleaseByHashUnknownHashIsAMiss(t *testing.T) {
	t.Parallel()
	fingerprint := hashFingerprint{0x12}
	hash := fingerprint.hex()

	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v2/version_file/"+hash {
			t.Errorf("unexpected request %s %s", request.Method, request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       http.NoBody,
		}, nil
	})

	provider := newModrinthTestProvider(t, transport)
	release, matched, err := provider.ReleaseByHash(t.Context(), fingerprint)
	if err != nil {
		t.Fatalf("release by hash: %v", err)
	}
	if matched {
		t.Fatalf("unknown digest matched %+v", release)
	}
}

func TestReleasesByHashMatchesExactSecondaryFile(t *testing.T) {
	t.Parallel()
	found := hashFingerprint{0x51}
	missing := hashFingerprint{0x52}

	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodPost &&
			request.URL.Path == "/v2/version_files":
			return jsonResponse(t, map[string]versionResponse{
				found.hex(): {
					ID:            "version-secondary",
					ProjectId:     "project-secondary",
					VersionNumber: "1.2.3",
					Files: []fileResponse{
						fileWithSha1(
							"primary-file",
							"primary.jar",
							strings.Repeat("0", 40),
							true,
						),
						fileWithSha1(
							"secondary-file",
							"secondary.jar",
							strings.ToUpper(found.hex()),
							false,
						),
					},
				},
			}), nil
		case request.Method == http.MethodGet &&
			request.URL.Path == "/v2/projects":
			return jsonResponse(t, []projectResponse{{
				ID:   "project-secondary",
				Slug: "secondary",
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
		[]upstream.ArtifactFingerprint{found, missing},
	)

	if results[0].Err != nil {
		t.Fatalf("matched digest error = %v", results[0].Err)
	}
	if !results[0].Matched {
		t.Fatal("expected the digest to match a release")
	}
	if results[0].Release.Candidate.Filename != "secondary.jar" {
		t.Errorf(
			"matched filename = %q, want the file carrying the digest",
			results[0].Release.Candidate.Filename,
		)
	}
	if results[0].Release.Candidate.FileID != "secondary-file" {
		t.Errorf(
			"matched file id = %q, want secondary-file",
			results[0].Release.Candidate.FileID,
		)
	}
	if results[1].Matched || results[1].Err != nil {
		t.Fatalf("omitted digest = %+v, want a clean miss", results[1])
	}
}

func TestReleasesByHashMismatchedFileIsAnError(t *testing.T) {
	t.Parallel()
	bad := hashFingerprint{0x53}
	missing := hashFingerprint{0x54}

	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.Method == http.MethodPost &&
			request.URL.Path == "/v2/version_files":
			return jsonResponse(t, map[string]versionResponse{
				bad.hex(): {
					ID:        "version-wrong-file",
					ProjectId: "project-wrong-file",
					Files: []fileResponse{
						fileWithSha1(
							"primary-file",
							"primary.jar",
							strings.Repeat("0", 40),
							true,
						),
					},
				},
			}), nil
		case strings.HasPrefix(request.URL.Path, "/v2/projects"):
			t.Error("project fetch for a digest with no matching file")
			return jsonResponse(t, []projectResponse{}), nil
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
		[]upstream.ArtifactFingerprint{bad, missing},
	)

	if results[0].Matched ||
		!errors.Is(results[0].Err, ErrInvalidAPIResponse) {
		t.Fatalf(
			"match without its file = %+v, want an invalid-response error",
			results[0],
		)
	}
	if !strings.Contains(results[0].Err.Error(), bad.hex()) {
		t.Errorf(
			"file error %q does not name hash %s",
			results[0].Err,
			bad.hex(),
		)
	}
	if results[1].Matched || results[1].Err != nil {
		t.Fatalf("omitted digest = %+v, want a clean miss", results[1])
	}
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
