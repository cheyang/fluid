#!/usr/bin/env bash
# Layer 2 (integration, network): the pinned github/codeql-action SHA should be
# the tip of the upstream v4 release line (refs/heads/releases/v4 == the peeled
# commit of the newest v4.x.y tag). A dependabot bump that lands behind the tip
# is already stale. Emits go-test -json style lines for scripts/re-verify.sh.
set -u
cd "$(git rev-parse --show-toplevel 2>/dev/null || echo .)"
UPSTREAM="https://github.com/github/codeql-action.git"

emit() { printf '{"Test":"%s","Action":"%s"}\n' "$1" "$2"; }

refs="$(git ls-remote "$UPSTREAM" 'refs/heads/releases/v4' 'refs/tags/v4.*^{}' 2>/dev/null)"
if [ -z "$refs" ]; then
  printf '{"Test":"IntegrationLayerRequiresNetwork","Action":"output","Output":"cannot reach %s; layer skipped"}\n' "$UPSTREAM"
  exit 2
fi
v4branch="$(printf '%s\n' "$refs" | awk '$2=="refs/heads/releases/v4"{print $1}')"
# newest semver tag = highest version-sort tag that peels to the v4 branch tip
tiptag="$(printf '%s\n' "$refs" | awk -v s="$v4branch" '$1==s && $2 ~ /refs\/tags\/v4\.[0-9]+\.[0-9]+\^\{\}$/ {t=$2; sub(/refs\/tags\//,"",t); sub(/\^\{\}$/,"",t); print t}' | sort -V | tail -1)"

rc=0
grep -RnE 'uses: github/codeql-action/[a-z-]+@[0-9a-f]{40}' .github/workflows/ | while IFS= read -r hit; do
  loc="$(printf '%s' "$hit" | cut -d: -f1,2)"
  line="$(printf '%s' "$hit" | cut -d: -f3-)"
  sha="$(printf '%s' "$line" | sed -E 's/.*@([0-9a-f]{40}).*/\1/')"
  if [ "$sha" = "$v4branch" ]; then
    emit "PinnedIsV4Tip/$loc" "pass"
  else
    emit "PinnedIsV4Tip/$loc" "fail"
    printf '{"Test":"PinnedIsV4Tip/%s","Action":"output","Output":"pinned %s is behind v4 tip %s (%s)"}\n' "$loc" "$sha" "$v4branch" "$tiptag"
  fi
done
[ -n "$v4branch" ] && grep -qE "uses: github/codeql-action/[a-z-]+@${v4branch}" -R .github/workflows/ || rc=1
exit $rc
