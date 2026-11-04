#!/bin/sh
# Materializes the pinned glamour module with a subset syntax highlighter.
#
# Size-trimmed builds pass -modfile=.build/glamour/build.mod; the alternate
# module file replaces glamour with the generated copy. Plain go commands keep
# using the upstream module, so the patch never hides what a dependency does.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$root"

out=.build/glamour
module=$out/module
go mod download charm.land/glamour/v2 github.com/alecthomas/chroma/v2
source_dir=$(go list -m -f '{{.Dir}}' charm.land/glamour/v2)
chroma_dir=$(go list -m -f '{{.Dir}}' github.com/alecthomas/chroma/v2)
if [ -z "$source_dir" ] || [ ! -d "$source_dir" ]; then
  echo "glamour module directory is unavailable" >&2
  exit 1
fi
if [ -z "$chroma_dir" ] || [ ! -d "$chroma_dir" ]; then
  echo "chroma module directory is unavailable" >&2
  exit 1
fi

chmod -R u+w "$out" 2>/dev/null || true
rm -rf "$out"
mkdir -p "$module/ansi/lexers" "$module/styles" "$module/internal/autolink"

cp "$source_dir/go.mod" "$source_dir/LICENSE" "$source_dir/glamour.go" "$module/"
for file in "$source_dir"/ansi/*.go; do
  case "$file" in
    *_test.go) ;;
    *) cp "$file" "$module/ansi/" ;;
  esac
done
for file in "$source_dir"/styles/*.go; do
  case "$file" in
    *_test.go) ;;
    *) cp "$file" "$module/styles/" ;;
  esac
done
cp "$source_dir"/internal/autolink/*.go "$module/internal/autolink/"

# The selected languages. Adding one also adds its XML and aliases to the
# embedded registry; every other lexer stays out of the build.
for name in \
  bash groovy html ini java javascript json kotlin lua plaintext properties \
  python sql toml typescript xml yaml; do
  cp "$chroma_dir/lexers/embedded/$name.xml" "$module/ansi/lexers/"
done

# The module cache is read-only; the patch overwrites one of its copies.
chmod -R u+w "$module"
for file in _patches/glamour/*.go; do
  cp "$file" "$module/ansi/"
done

cp go.mod "$out/build.mod"
cp go.sum "$out/build.sum"
go mod edit -replace=charm.land/glamour/v2="$root/$module" "$out/build.mod"
