package modrinth

import (
	"strings"
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
	Value     string
}

func (f *facetItem) String() string {
	return `"` + f.Type + f.Operation.String() + f.Value + `"`
}

// facetItems is an array of facetItem. It represents an expression joined by OR statements.
// a complete facet is an array of facetItems, with each array joined by AND statements.
type facetItems []facetItem

// There are no facet data structures, rather, a function is used to directly
// create a facet string that can be used in the URL.
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
		Value:     "forge",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Value:     "fabric",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Value:     "quilt",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Value:     "liteloader",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Value:     "modloader",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Value:     "rift",
	},
	{
		Type:      "categories",
		Operation: operationEq,
		Value:     "neoforge",
	},
}

var facetForge = facetItems{
	{
		Type:      "categories",
		Operation: operationEq,
		Value:     "forge",
	},
}

var facetFabric = facetItems{
	{
		Type:      "categories",
		Operation: operationEq,
		Value:     "fabric",
	},
}

var facetServerSupported = facetItems{
	{
		Type:      "server_side",
		Operation: operationEq,
		Value:     "required",
	},
	{
		Type:      "server_side",
		Operation: operationEq,
		Value:     "optional",
	},
}

var facetClientSupported = facetItems{
	{
		Type:      "client_side",
		Operation: operationEq,
		Value:     "required",
	},
	{
		Type:      "client_side",
		Operation: operationEq,
		Value:     "optional",
	},
}

var facetBothRequired = []facetItems{
	{
		{
			Type:      "server_side",
			Operation: operationEq,
			Value:     "required",
		},
	},
	{
		{
			Type:      "client_side",
			Operation: operationEq,
			Value:     "required",
		},
	},
}

var facetBothSupported = []facetItems{
	{
		{
			Type:      "server_side",
			Operation: operationEq,
			Value:     "required",
		},
		{
			Type:      "server_side",
			Operation: operationEq,
			Value:     "optional",
		},
	},
	{
		{
			Type:      "client_side",
			Operation: operationEq,
			Value:     "required",
		},
		{
			Type:      "client_side",
			Operation: operationEq,
			Value:     "optional",
		},
	},
}
