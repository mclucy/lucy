#!/bin/bash
set -euo pipefail

COPYRIGHT='/*
Copyright 2024 4rcadia

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
'

MODE="${1:-add}"

cd "$(dirname "$0")" || exit 1

header_file="$(mktemp)"
printf "%s" "$COPYRIGHT" > "$header_file"
copyright_line_count=$(wc -l < "$header_file" | tr -d ' ')
trap 'rm -f "$header_file"' EXIT

apply_add() {
    local file="$1"
    if grep -q "Copyright 2024 4rcadia" "$file"; then
        return 0
    fi

    {
        printf "%s" "$COPYRIGHT"
        cat "$file"
    } > "${file}.tmp"
    mv "${file}.tmp" "$file"
}

apply_remove() {
    local file="$1"
    if ! grep -q "Copyright 2024 4rcadia" "$file"; then
        return 0
    fi

    if ! head -n "$copyright_line_count" "$file" | cmp -s - "$header_file"; then
        return 0
    fi

    tail -n +$((copyright_line_count + 1)) "$file" > "${file}.tmp"
    mv "${file}.tmp" "$file"
}

find . -name "*.go" -type f -not -path "*/\.*" | while read -r file; do
    case "$MODE" in
        add)
            apply_add "$file"
            ;;
        remove)
            apply_remove "$file"
            ;;
        *)
            echo "Usage: $0 [add|remove]" >&2
            exit 2
            ;;
    esac
done
