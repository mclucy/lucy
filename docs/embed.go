// Package docs embeds the documentation pages used by lucy docs.
package docs

import "embed"

// Files holds the site pages. Metadata, assets, and drafts stay out.
//
//go:embed *.mdx commands/*.mdx file-tracking/*.mdx
var Files embed.FS
