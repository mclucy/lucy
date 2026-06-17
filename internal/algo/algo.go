// Package algo contains pure mathematical algorithms with no Lucy domain dependencies.
package algo

// JaroWinklerSimilarity returns 0.0-1.0, with 1.0 being a perfect match and
// 0.0 being no match.
func JaroWinklerSimilarity(s1, s2 string) float64 {
	if len(s1) == 0 && len(s2) == 0 {
		return 1.0
	}

	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}

	matchDistance := max(len(s1), len(s2))/2 - 1
	matchDistance = max(matchDistance, 0)

	s1Matches := make([]bool, len(s1))
	s2Matches := make([]bool, len(s2))

	var matchingCharacters float64

	for i := 0; i < len(s1); i++ {
		start := max(0, i-matchDistance)
		end := min(i+matchDistance+1, len(s2))

		for j := start; j < end; j++ {
			if !s2Matches[j] && s1[i] == s2[j] {
				s1Matches[i] = true
				s2Matches[j] = true
				matchingCharacters++
				break
			}
		}
	}

	if matchingCharacters == 0 {
		return 0.0
	}

	var transpositions float64
	var point float64

	for i := 0; i < len(s1); i++ {
		if s1Matches[i] {
			for j := int(point); j < len(s2); j++ {
				if s2Matches[j] {
					point = float64(j) + 1
					break
				}
			}

			if s1[i] != s2[int(point)-1] {
				transpositions++
			}
		}
	}

	transpositions /= 2
	jaroSimilarity := (matchingCharacters/float64(len(s1)) +
		matchingCharacters/float64(len(s2)) +
		(matchingCharacters-transpositions)/matchingCharacters) / 3.0

	const commonPrefixLength = 4
	var prefixLength float64
	for i := 0; i < min(len(s1), len(s2), commonPrefixLength); i++ {
		if s1[i] == s2[i] {
			prefixLength++
		} else {
			break
		}
	}

	const winklerScalingFactor = 0.1
	return jaroSimilarity + prefixLength*winklerScalingFactor*(1-jaroSimilarity)
}

func NormalizedLevenshteinDistance(s1, s2 string) float64 {
	maxLen := max(len(s1), len(s2))
	if maxLen == 0 {
		return 0
	}
	distance := LevenshteinDistance(s1, s2)
	return float64(distance) / float64(maxLen)
}

func LevenshteinDistance(s1, s2 string) int {
	m := len(s1)
	n := len(s2)

	distances := make([][]int, m+1)
	for i := range distances {
		distances[i] = make([]int, n+1)
	}

	for i := 0; i <= m; i++ {
		distances[i][0] = i
	}

	for j := 0; j <= n; j++ {
		distances[0][j] = j
	}

	for j := 1; j <= n; j++ {
		for i := 1; i <= m; i++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}

			distances[i][j] = min(
				distances[i-1][j]+1,
				distances[i][j-1]+1,
				distances[i-1][j-1]+cost,
			)
		}
	}

	return distances[m][n]
}
