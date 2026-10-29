package cache

import "testing"

func TestEntryKindString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind EntryKind
		want string
	}{
		{kind: KindMetadata, want: "metadata"},
		{kind: KindArtifact, want: "artifact"},
		{kind: EntryKind(255), want: "unknown"},
	}
	for _, test := range tests {
		if got := test.kind.String(); got != test.want {
			t.Fatalf("EntryKind(%d).String() = %q, want %q", test.kind, got, test.want)
		}
	}
}

func TestHashAlgorithmString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		algorithm HashAlgorithm
		want      string
	}{
		{algorithm: HashNone, want: "none"},
		{algorithm: HashSHA1, want: "sha1"},
		{algorithm: HashSHA256, want: "sha256"},
		{algorithm: HashSHA512, want: "sha512"},
		{algorithm: HashAlgorithm(255), want: "none"},
	}
	for _, test := range tests {
		if got := test.algorithm.String(); got != test.want {
			t.Fatalf("HashAlgorithm(%d).String() = %q, want %q", test.algorithm, got, test.want)
		}
	}
}

func TestParseHashAlgorithm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  HashAlgorithm
	}{
		{value: "sha1", want: HashSHA1},
		{value: "sha256", want: HashSHA256},
		{value: "sha512", want: HashSHA512},
		{value: "", want: HashNone},
		{value: "SHA256", want: HashNone},
		{value: "md5", want: HashNone},
	}
	for _, test := range tests {
		if got := ParseHashAlgorithm(test.value); got != test.want {
			t.Fatalf("ParseHashAlgorithm(%q) = %v, want %v", test.value, got, test.want)
		}
	}
}

func TestIntegrityStateString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state IntegrityState
		want  string
	}{
		{state: IntegrityUnverified, want: "unverified"},
		{state: IntegrityVerified, want: "verified"},
		{state: IntegrityState(255), want: "unknown"},
	}
	for _, test := range tests {
		if got := test.state.String(); got != test.want {
			t.Fatalf("IntegrityState(%d).String() = %q, want %q", test.state, got, test.want)
		}
	}
}
