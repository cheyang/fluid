#!/bin/bash
# Contract test (polarity: contract — FAILS on the current head commit, PASSES once the
# Signed-off-by line matches the commit author).
#
# Claim F2: "the PR's DCO check is red because the commit's Signed-off-by name
# (`cheyang.cy`) does not match the commit author name (`cheyang`); probot/dco requires
# name AND email to match."
#
# Mirrors the DCO rule for every commit in <base>..<head>: author name+email must equal
# the last Signed-off-by name+email. Usage: bash check_dco.sh [base..range]
set -u
RANGE="${1:-dcb76167b0143a239addc6bed83ed50b45267631..HEAD}"

fail=0
for c in $(git rev-list "$RANGE"); do
  author_name=$(git log -1 --format='%an' "$c")
  author_mail=$(git log -1 --format='%ae' "$c")
  sobo=$(git log -1 --format='%B' "$c" | grep -i '^Signed-off-by:' | tail -1 | sed 's/^[Ss]igned-[Oo]ff-[Bb]y:[[:space:]]*//')
  if [ -z "$sobo" ]; then
    echo "FAIL ${c}: no Signed-off-by line"; fail=1; continue
  fi
  sobo_name="${sobo% <*}"
  sobo_mail="${sobo##*<}"; sobo_mail="${sobo_mail%>}"
  if [ "$sobo_name" = "$author_name" ] && [ "$sobo_mail" = "$author_mail" ]; then
    echo "OK   ${c}: author '${author_name} <${author_mail}>' == sign-off"
  else
    echo "FAIL ${c}: author '${author_name} <${author_mail}>' != sign-off '${sobo_name} <${sobo_mail}>'"
    fail=1
  fi
done
exit $fail
