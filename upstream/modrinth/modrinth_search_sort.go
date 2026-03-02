package modrinth

import "github.com/mclucy/lucy/types"

func modrinthSearchSortingString(sort types.SearchSort) string {
	switch sort {
	case types.SearchSortRelevance:
		return "relevance"
	case types.SearchSortDownloads:
		return "downloads"
	case types.SearchSortNewest:
		return "newest"
	default:
		return "relevance"
	}
}
