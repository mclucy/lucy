package modrinth

import (
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
)

type searchOptions struct {
	index  string
	facets []facetItems
}

type facetItemOperation uint8

const (
	operationEq facetItemOperation = iota
	operationNeq
	operationLeq
	operationGeq
	operationLt
	operationGt
)

func (op facetItemOperation) String() string {
	switch op {
	case operationEq:
		return ":"
	case operationNeq:
		return "!="
	case operationLeq:
		return "<="
	case operationGeq:
		return ">="
	case operationLt:
		return "<"
	case operationGt:
		return ">"
	default:
		return ""
	}
}

// facet is the data structure to construct an advanced Modrinth search. It
// does not contain all the search options, only the ones that are expected in this
// program.
//
// From Modrinth docs:
//
// In order to then use these facets, you need a value to filter by, as well as
// an operation to perform on this value. The most common operation is ':'
// (same as =), though you can also use !=, >=, >, <=, and <. Join together the
// type, operation, and value, and you’ve got your string.
//
// {type} {operation} {value}
//
// categories = adventure
// versions != 1.20.1
// downloads <= 100
//
// You then join these strings together in arrays to signal AND OR operators.
//
// OR
// All elements in a single array are considered to be joined by OR statements.
// For example, the search [["versions:1.16.5", "versions:1.17.1"]] translates
// to Projects that support 1.16.5 OR 1.17.1.
//
// AND
// Separate arrays are considered to be joined by AND statements. For example,
// the search [["versions:1.16.5"], ["project_type:modpack"]] translates to
// Projects that support 1.16.5 AND are modpacks.
//
// API Docs: https://docs.modrinth.com/api/operations/searchprojects/
type facetItem struct {
	Type      string
	Operation facetItemOperation
	Match     string
}

func (f *facetItem) String() string {
	return `"` + f.Type + f.Operation.String() + f.Match + `"`
}

// facetItems is an array of facetItem. It represents an expression joined by
// OR statements. A complete facet is an array of facetItems, with each array
// joined by AND statements.
type facetItems []facetItem

// serializeFacet renders expressions as Modrinth's nested JSON arrays; see
// facetItems for the OR/AND semantics.
func serializeFacet(expressions ...facetItems) string {
	var sb strings.Builder
	sb.WriteRune('[')
	for i, expression := range expressions {
		if i > 0 {
			sb.WriteRune(',')
		}
		sb.WriteRune('[')
		for j, item := range expression {
			if j > 0 {
				sb.WriteRune(',')
			}
			sb.WriteString(item.String())
		}
		sb.WriteRune(']')
	}
	sb.WriteRune(']')
	return sb.String()
}

var facetAllLoaders = facetItems{
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "forge",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "fabric",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "quilt",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "liteloader",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "modloader",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "rift",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "neoforge",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "bukkit",
	},
}

var facetForgeOnly = facetItems{
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "forge",
	},
}

var facetFabricOnly = facetItems{
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "fabric",
	},
}

// facetQuiltOnly selects projects published under the Quilt loader category.
var facetQuiltOnly = facetItems{
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "quilt",
	},
}

var facetNeoforgeOnly = facetItems{
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "neoforge",
	},
}

var facetBukkitOnly = facetItems{
	{
		Type:      "categories",
		Operation: operationEq,
		Match:     "bukkit",
	},
}

// loaderFacetsByEcosystem maps an ecosystem to the loader category that
// identifies its packages. An ecosystem absent from the table searches every
// loader.
var loaderFacetsByEcosystem = map[catalog.Ecosystem]facetItems{
	catalog.EcosystemForge:    facetForgeOnly,
	catalog.EcosystemFabric:   facetFabricOnly,
	catalog.EcosystemQuilt:    facetQuiltOnly,
	catalog.EcosystemNeoForge: facetNeoforgeOnly,
	catalog.EcosystemBukkit:   facetBukkitOnly,
}

// loaderFacets returns the loader categories a runtime of ecosystem loads, in
// descriptor precedence order. A runtime that loads more than one ecosystem
// (Quilt loads Fabric) matches their union.
func loaderFacets(ecosystem catalog.Ecosystem) facetItems {
	facets, ok := loaderFacetsByEcosystem[ecosystem]
	if !ok {
		return facetAllLoaders
	}
	facets = slices.Clone(facets)
	for _, loaded := range ecosystem.Loads() {
		if loaded == ecosystem {
			continue
		}
		facets = append(facets, loaderFacetsByEcosystem[loaded]...)
	}
	return facets
}

var facetServerSupported = facetItems{
	{
		Type:      "server_side",
		Operation: operationEq,
		Match:     "required",
	},
	{
		Type:      "server_side",
		Operation: operationEq,
		Match:     "optional",
	},
}

var facetClientSupported = facetItems{
	{
		Type:      "client_side",
		Operation: operationEq,
		Match:     "required",
	},
	{
		Type:      "client_side",
		Operation: operationEq,
		Match:     "optional",
	},
}

var facetBothRequired = []facetItems{
	{
		{
			Type:      "server_side",
			Operation: operationEq,
			Match:     "required",
		},
	},
	{
		{
			Type:      "client_side",
			Operation: operationEq,
			Match:     "required",
		},
	},
}

var facetBothSupported = []facetItems{
	{
		{
			Type:      "server_side",
			Operation: operationEq,
			Match:     "required",
		},
		{
			Type:      "server_side",
			Operation: operationEq,
			Match:     "optional",
		},
	},
	{
		{
			Type:      "client_side",
			Operation: operationEq,
			Match:     "required",
		},
		{
			Type:      "client_side",
			Operation: operationEq,
			Match:     "optional",
		},
	},
}
