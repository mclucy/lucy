package algo

import "testing"

func TestLevenshteinDistance(t *testing.T) {
	if got := LevenshteinDistance("kitten", "sitting"); got != 3 {
		t.Fatalf("LevenshteinDistance() = %d, want 3", got)
	}
}

func TestNormalizedLevenshteinDistance(t *testing.T) {
	if got := NormalizedLevenshteinDistance("kitten", "sitting"); got != 3.0/7.0 {
		t.Fatalf("NormalizedLevenshteinDistance() = %f, want %f", got, 3.0/7.0)
	}
}

func TestJaroWinklerSimilarity(t *testing.T) {
	if got := JaroWinklerSimilarity("martha", "marhta"); got <= 0.96 || got >= 0.97 {
		t.Fatalf("JaroWinklerSimilarity() = %f, want approximately 0.961", got)
	}
}
