// Package docs embeds the documentation pages used by lucy docs.
package docs

import _ "embed"

// Archive holds the compressed site pages. Metadata, assets, and drafts stay out.
//
//go:embed docs.tar.gz
var Archive []byte
