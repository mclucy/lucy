#!/usr/bin/env bash
set -euo pipefail

# Builds the pull request and base binaries for every published target and
# writes the size report to /tmp/report.md. The workflow mounts the checkouts as
# siblings: pr/ for the pull request and main/ for the base, and this script
# lives in the pull request checkout.

cd "$(dirname "$0")/.." || exit 1

pr_root=$PWD
main_root="$(dirname "$pr_root")/main"
pr_bins=/tmp/pr-bins
main_bins=/tmp/main-bins
report=/tmp/report.md

targets() {
  cat <<'EOF'
linux-amd64 linux amd64
linux-arm64 linux arm64
darwin-arm64 darwin arm64
darwin-amd64 darwin amd64
windows-amd64 windows amd64 .exe
EOF
}

build_checkout() {
  local root=$1 out=$2 name goos goarch ext
  (cd "$root" && go run ./tools/gendocs)
  mkdir -p "$out"
  while read -r name goos goarch ext; do
    (
      cd "$root"
      GOOS="$goos" GOARCH="$goarch" go build -trimpath \
        -ldflags "-w -s" \
        -tags release,docsarchive,nethttpomithttp2 \
        -o "${out}/lucy-${name}${ext}"
    )
  done < <(targets)
}

file_size() {
  wc -c < "$1" | tr -d "[:space:]"
}

group_digits() {
  local digits=$1 grouped=""
  while [ "${#digits}" -gt 3 ]; do
    grouped=",${digits: -3}${grouped}"
    digits="${digits:0:${#digits}-3}"
  done
  printf "%s%s" "$digits" "$grouped"
}

format_size() {
  local size=$1 sign="" hundredths tenths
  if [ "$size" -lt 0 ]; then
    sign="-"
    size=$((-size))
  fi
  if [ "$size" -ge 1048576 ]; then
    hundredths=$(((size * 100 + 524288) / 1048576))
    printf "%s%d.%02d MB" "$sign" "$((hundredths / 100))" "$((hundredths % 100))"
  elif [ "$size" -ge 1024 ]; then
    tenths=$(((size * 10 + 512) / 1024))
    printf "%s%d.%d KB" "$sign" "$((tenths / 10))" "$((tenths % 10))"
  else
    printf "%s%d B" "$sign" "$size"
  fi
}

build_checkout "$pr_root" "$pr_bins"
build_checkout "$main_root" "$main_bins"

rows=""
while read -r name goos goarch ext; do
  base="$(file_size "${main_bins}/lucy-${name}${ext}")"
  head="$(file_size "${pr_bins}/lucy-${name}${ext}")"
  diff=$((head - base))

  sign=""
  if [ "$diff" -gt 0 ]; then
    sign="+"
  fi
  abs_diff=$diff
  if [ "$abs_diff" -lt 0 ]; then
    abs_diff=$((-abs_diff))
  fi
  pct_hundredths=0
  if [ "$base" -gt 0 ]; then
    pct_hundredths=$(((abs_diff * 20000 + base) / (2 * base)))
  fi
  pct_sign=""
  if [ "$diff" -lt 0 ]; then
    pct_sign="-"
  fi
  pct="$(printf "%d.%02d" "$((pct_hundredths / 100))" "$((pct_hundredths % 100))")"

  indicator="⚪"
  if [ "$diff" -lt 0 ]; then
    indicator="🟢"
  elif [ "$diff" -gt 0 ]; then
    indicator="🔴"
  fi

  rows="${rows}| \`${name}\` | $(format_size "$base") ($(group_digits "$base") B) | $(format_size "$head") ($(group_digits "$head") B) | **${sign}$(format_size "$diff") (${pct_sign}${pct}%)** ${indicator} |
"
done < <(targets)

body="### 📦 Binary Size Report (compared against \`main\`)

| Target | \`main\` | PR | Change |
| :--- | :--- | :--- | :--- |
${rows}
*Automated check triggered by changes to \`go.mod\` / \`go.sum\`.*
<!-- lucy-binary-size-report -->
"
printf "%s" "$body" > "$report"
if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  printf "%s" "$body" >> "$GITHUB_STEP_SUMMARY"
fi
