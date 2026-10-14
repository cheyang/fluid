#!/usr/bin/env bash
# 10-verify-pin.sh — supply-chain + drift checks for the helm/kind-action pin
# bumped by https://github.com/fluid-cloudnative/fluid/pull/6208.
#
# Emits "<name>\tpass|fail" lines (parsed by re-verify.sh, kind=shell).
# NOTE for re-verify.sh: its lookup keeps the LAST line matching each
# substring, so every group ends with an "[all]" summary line.
#   yaml-pin-tag-match[<file>] / [all]  pinned SHA == upstream commit of the
#                                       tag in the "# vX.Y.Z" comment
#   yaml-pin-drift                      single distinct sha across the repo
#   old-pin-absent                      pre-PR v1.15.0 sha no longer referenced
#
# Usage: 10-verify-pin.sh [REPO_ROOT]   (default: git top-level of cwd)
set -uo pipefail

REPO_ROOT="${1:-$(git rev-parse --show-toplevel 2>/dev/null || echo .)}"
OLD_SHA="06c1ae10762d3b9c1644e7fe69596ae519e015a2"   # helm/kind-action v1.15.0
ACTION_REPO="https://github.com/helm/kind-action.git"
WF_DIR="$REPO_ROOT/.github/workflows"

emit() { printf '%s\t%s\n' "$1" "$2"; }

# Extract "file<TAB>sha<TAB>tag" for every helm/kind-action consumer.
PINS="$(grep -rE 'helm/kind-action@[0-9a-f]{40}[[:space:]]*#[[:space:]]*v[0-9]+\.[0-9]+\.[0-9]+' "$WF_DIR" 2>/dev/null \
  | awk '{ file=$1; sub(/:$/, "", file); sha=""; tag="";
           for (i=1; i<=NF; i++) {
             if ($i ~ /^helm\/kind-action@[0-9a-f]{40}$/) { sha=$i; sub(/.*@/, "", sha) }
             if ($i ~ /^v[0-9]+\.[0-9]+\.[0-9]+$/) { tag=$i }
           }
           if (sha != "" && tag != "") printf "%s\t%s\t%s\n", file, sha, tag }')"

anyfail=0
if [ -z "$PINS" ]; then
  emit "yaml-pin-tag-match[all]" "fail"
  emit "yaml-pin-drift" "fail"
  echo "no helm/kind-action pins found under $WF_DIR" >&2
else
  while IFS="$(printf '\t')" read -r f sha tag; do
    f="$(basename "$f")"
    # Resolve the upstream tag -> commit. Prefer the peeled ^{} line (annotated
    # tags); fall back to the tag ref itself (lightweight tags).
    remote="$(git ls-remote "$ACTION_REPO" "refs/tags/${tag}^{}" "refs/tags/${tag}" 2>/dev/null)"
    tagged="$(printf '%s\n' "$remote" | grep '\^{}' | cut -f1 | head -n1)"
    [ -n "$tagged" ] || tagged="$(printf '%s\n' "$remote" | cut -f1 | head -n1)"
    if [ -n "$tagged" ] && [ "$tagged" = "$sha" ]; then
      emit "yaml-pin-tag-match[$f]" "pass"
      echo "OK   $f: $sha == $ACTION_REPO $tag" >&2
    else
      emit "yaml-pin-tag-match[$f]" "fail"
      anyfail=1
      echo "BAD  $f: pinned $sha but $tag resolves to ${tagged:-<unresolvable>}" >&2
    fi
  done < <(printf '%s\n' "$PINS")
  # summary line LAST for this group (re-verify.sh keeps the last match)
  if [ "$anyfail" = "0" ]; then emit "yaml-pin-tag-match[all]" "pass"; else emit "yaml-pin-tag-match[all]" "fail"; fi

  npins="$(printf '%s\n' "$PINS" | wc -l | tr -d ' ')"
  distinct="$(printf '%s\n' "$PINS" | cut -f2 | sort -u | wc -l | tr -d ' ')"
  echo "pins found: $npins, distinct shas: $distinct" >&2
  if [ "$distinct" = "1" ]; then
    emit "yaml-pin-drift" "pass"
  else
    emit "yaml-pin-drift" "fail"
    echo "BAD  $distinct distinct helm/kind-action shas pinned across workflows" >&2
  fi
fi

if grep -rqF "$OLD_SHA" "$WF_DIR" 2>/dev/null; then
  emit "old-pin-absent" "fail"
  echo "BAD  stale v1.15.0 sha $OLD_SHA still referenced" >&2
else
  emit "old-pin-absent" "pass"
fi
