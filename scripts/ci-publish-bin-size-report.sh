#!/usr/bin/env bash
set -euo pipefail

# Publishes the size report as one PR comment, replacing the previous report in
# place. Failures stay non-fatal: the step summary already carries the result.

: "${PR_NUMBER:?PR_NUMBER is not set}"
: "${GH_REPO:?GH_REPO is not set}"

report=/tmp/report.md
comment_id="$(gh api --paginate "repos/${GH_REPO}/issues/${PR_NUMBER}/comments" \
  --jq '.[] | select(.body | contains("<!-- lucy-binary-size-report -->")) | .id' | head -n 1 || true)"
if [ -n "$comment_id" ]; then
  gh api -X PATCH "repos/${GH_REPO}/issues/comments/${comment_id}" -F body=@"${report}" || true
else
  gh pr comment "$PR_NUMBER" --repo "$GH_REPO" --body-file "$report" || true
fi
