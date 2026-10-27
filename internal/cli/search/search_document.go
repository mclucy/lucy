package search

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/upstream"
)

// searchResultDocument is the JSON wire shape for one provider's results.
// Warnings become messages because the error values themselves have no
// consumer-meaningful JSON representation.
type searchResultDocument struct {
	Provider catalog.Provider        `json:"provider"`
	Items    []upstream.SearchResult `json:"items"`
	Warnings []string                `json:"warnings,omitempty"`
}

var searchJSON cli.JSONResult[[]searchResultDocument]

func newSearchResultDocuments(
	results []upstream.SearchResponse,
) []searchResultDocument {
	if results == nil {
		return nil
	}
	documents := make([]searchResultDocument, len(results))
	for i, result := range results {
		documents[i] = searchResultDocument{
			Provider: result.Provider,
			Items:    result.Items,
			Warnings: searchWarningMessages(result.Warnings),
		}
	}
	return documents
}

func searchWarningMessages(warnings []error) []string {
	if len(warnings) == 0 {
		return nil
	}
	messages := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning == nil {
			continue
		}
		messages = append(messages, warning.Error())
	}
	if len(messages) == 0 {
		return nil
	}
	return messages
}
