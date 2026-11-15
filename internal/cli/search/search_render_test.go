package search

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/mclucy/lucy/upstream"
)

func TestRenderSearchCompactFitsWidth(t *testing.T) {
	t.Parallel()

	response := upstream.SearchResponse{
		Provider: catalog.ProviderModrinth,
		Items: []upstream.SearchResult{
			{
				ProjectName: "purpurpacks-more-dyed-wool-and-carpet",
				Provider:    catalog.ProviderModrinth,
				Description: strings.Repeat("word ", 20),
			},
			{
				ProjectName: "carpet",
				Provider:    catalog.ProviderModrinth,
				Description: "Take full control over your vanilla game",
			},
		},
	}
	for _, width := range []int{40, 60, 100, 140} {
		rendered := renderSearchCompact(
			response,
			style.New(false),
			style.Terminal{Width: width},
		)
		for _, line := range strings.Split(strings.TrimRight(rendered, "\n"), "\n") {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf(
					"width %d: line is %d cells wide: %q",
					width,
					got,
					ansi.Strip(line),
				)
			}
		}
	}
}
