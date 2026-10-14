#!/usr/bin/env bash
# Layer 2 (integration, network): resolve the pinned github/codeql-action SHAs
# against the upstream repo's tags.
#   - PinResolvesToUpstreamTag/<sha>:  the pinned SHA is the commit of some
#     release tag (supply-chain authenticity).
#   - VersionCommentMatchesPinnedTag/<file>:<line>: the trailing "# vX.Y.Z"
#     comment equals the tag the pinned SHA actually resolves to.
# Emits go-test -json style lines for scripts/re-verify.sh.
set -u
cd "$(git rev-parse --show-toplevel 2>/dev/null || echo .)"
UPSTREAM="https://github.com/github/codeql-action.git"

emit() { printf '{"Test":"%s","Action":"%s"}\n' "$1" "$2"; }

tags="$(git ls-remote --tags "$UPSTREAM" 2>/dev/null)"
if [ -z "$tags" ]; then
  printf '{"Test":"IntegrationLayerRequiresNetwork","Action":"output","Output":"cannot reach %s; layer skipped"}\n' "$UPSTREAM"
  exit 2
fi
# map: commit-sha -> tag (dereference annotated tags via the ^{} lines)
# Prefer a full semver tag (vX.Y.Z) over moving major tags (v4) pointing at the same commit.
tag_of() { printf '%s\n' "$tags" | awk -v s="$1" '$1==s && $2 ~ /\^\{\}$/ {t=$2; sub(/refs\/tags\//,"",t); sub(/\^\{\}$/,"",t); if (t ~ /^v[0-9]+\.[0-9]+\.[0-9]+$/) {print t; found=1; exit} if (any=="") any=t} END {if (!found && any!="") print any}'; }

rc=0
grep -RnE 'uses: github/codeql-action/[a-z-]+@[0-9a-f]{40}' .github/workflows/ | while IFS= read -r hit; do
  loc="$(printf '%s' "$hit" | cut -d: -f1,2)"
  line="$(printf '%s' "$hit" | cut -d: -f3-)"
  sha="$(printf '%s' "$line" | sed -E 's/.*@([0-9a-f]{40}).*/\1/')"
  tag="$(tag_of "$sha")"
  if [ -n "$tag" ]; then
    emit "PinResolvesToUpstreamTag/$loc" "pass"
  else
    emit "PinResolvesToUpstreamTag/$loc" "fail"
    printf '{"Test":"PinResolvesToUpstreamTag/%s","Action":"output","Output":"pinned sha %s does not match any upstream release tag"}\n' "$loc" "$sha"
  fi
  comment="$(printf '%s' "$line" | grep -oE '#[[:space:]]*v[0-9]+\.[0-9]+\.[0-9]+' | head -1 | tr -d '# ')"
  if [ -z "$comment" ]; then
    emit "VersionCommentPresent/$loc" "fail"
    printf '{"Test":"VersionCommentPresent/%s","Action":"output","Output":"no trailing vX.Y.Z comment; audit value lost"}\n' "$loc"
  elif [ -n "$tag" ] && [ "$comment" = "$tag" ]; then
    emit "VersionCommentMatchesPinnedTag/$loc" "pass"
  else
    emit "VersionCommentMatchesPinnedTag/$loc" "fail"
    printf '{"Test":"VersionCommentMatchesPinnedTag/%s","Action":"output","Output":"comment says %s but pinned sha %s is tag %s"}\n' "$loc" "$comment" "$sha" "$tag"
  fi
done

# propagate failure: re-run lookup cheaply for exit code only
grep -RnE 'uses: github/codeql-action/[a-z-]+@[0-9a-f]{40}' .github/workflows/ | while IFS= read -r hit; do
  line="$(printf '%s' "$hit" | cut -d: -f3-)"
  sha="$(printf '%s' "$line" | sed -E 's/.*@([0-9a-f]{40}).*/\1/')"
  tag="$(tag_of "$sha")"
  comment="$(printf '%s' "$line" | grep -oE '#[[:space:]]*v[0-9]+\.[0-9]+\.[0-9]+' | head -1 | tr -d '# ')"
  [ -n "$tag" ] && [ "$comment" = "$tag" ] || exit 1
done || rc=1
exit $rc
