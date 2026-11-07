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
