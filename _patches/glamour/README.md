# glamour build patch

`charm.land/glamour/v2` imports `chroma/quick`, and `chroma/lexers` registers
its whole lexer registry during package init. That adds about 4 MB to a release
binary for fenced-code coloring, which Lucy only shows in package descriptions.

Instead of the full registry, this patch registers only the languages listed in
`materialize.sh` (18 XML lexers, about 110 KB of data). `highlight.go` registers
that subset in one chroma registry and applies the colors already declared by
the Markdown style;
`codeblock.go` keeps glamour's block layout and styling, and styles without a
chroma section (notty) keep the plain fallback. Fence tags resolve by lexer
name, alias, or filename glob (so `yml`, `gradle`, and `kts` work). Unknown
languages and fences without a language use the plaintext lexer. HTML delegates
script and style blocks to the JavaScript and CSS lexers; both are included and
share the subset registry so delegated lookups resolve.

`materialize.sh` copies the glamour module pinned in `go.mod` from the module
cache into `.build/glamour/module`, copies the selected lexer XML from the
pinned chroma module, applies this directory's `.go` files, and writes
`build.mod`/`build.sum` whose `replace` points at that copy.

`task build`, `task build:release`, `task run`, GoReleaser, and the e2e build use
`-modfile`, so they build with the subset. Plain `go build` and `go test` still
use the upstream module; the only behavioral difference is fenced-code coloring,
so tests and language servers keep seeing the dependency as published.

To add a language, add its XML name to the list in `materialize.sh`; if chroma
does not ship an XML file for it, it must become a Go lexer instead. To update
glamour or chroma, bump `go.mod`, delete `.build/glamour`, and run
`task glamour:patch --force`.

Upstream is MIT licensed; see `LICENSE`.
