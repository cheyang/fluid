#!/usr/bin/env bash
# run-checks.sh — L1 (deterministic, network read-only) harness for PR #6290
# (bump ossf/scorecard-action v2.5.0 -> v2.6.0 in .github/workflows/sonarcloud.yaml).
#
# It reads the workflow file from the CURRENT checkout (so it re-validates
# whatever ref it is grafted onto) and the upstream action sources at the pinned
# commit, then emits go-test-json events so the stock re-verify.sh can parse it.
#
# Tests:
#   PinMatchesTag           contract — the pinned SHA equals the commit the
#                                    version comment's tag dereferences to.
#   ImageManifestAvailable  contract — ghcr.io/ossf/scorecard-action:<tag> exists
#                                    and includes linux/amd64 (workflow runs on
#                                    ubuntu-22.04).
#   PublishResultsEffective contract — the pinned action version actually reads
#                                    INPUT_PUBLISH_RESULTS (v2.5.0 did NOT), and
#                                    the workflow sets publish_results: true.
#                                    Documents the behavioral activation (F2).
#   ExcludeInputsIgnored    canary  — asserts the CURRENT (wrong) behavior:
#                                    exclude_checks / exclude_paths are not
#                                    declared inputs of the action, so GitHub
#                                    drops them ("Unexpected input(s)") and the
#                                    action never reads them (F1, pre-existing).
#                                    Flips (fails) once the dead keys are removed
#                                    from the workflow OR the action declares them.
#
# Requires: curl, python3, network access to api.github.com and ghcr.io.
set -uo pipefail

WF=".github/workflows/sonarcloud.yaml"
API="https://api.github.com/repos/ossf/scorecard-action"

fail() { echo "harness error: $*" >&2; exit 2; }

emit() { # emit <Test> <pass|fail> <detail>
  local detail
  detail="$(printf '%s' "$3" | tr -d '"')" # keep the hand-rolled JSON valid
  printf '{"Action":"run","Test":"%s","Package":"verify"}\n' "$1"
  printf '{"Action":"output","Test":"%s","Package":"verify","Output":"%s\\n"}\n' "$1" "$detail"
  printf '{"Action":"%s","Test":"%s","Package":"verify","Elapsed":0}\n' "$2" "$1"
}

[ -f "$WF" ] || fail "workflow file $WF not found in $(pwd)"

# --- extract the scorecard step pin + version comment -----------------------
PIN_LINE="$(grep -n 'uses: ossf/scorecard-action@' "$WF" | head -1)"
PIN="$(printf '%s' "$PIN_LINE" | grep -oE '@[0-9a-f]{40}' | tr -d '@')"
TAG="$(printf '%s' "$PIN_LINE" | grep -oE '# v[0-9]+\.[0-9]+\.[0-9]+' | tr -d '# ')"
[ -n "$PIN" ] || fail "could not parse pinned SHA from: $PIN_LINE"
[ -n "$TAG" ] || fail "could not parse version comment from: $PIN_LINE"
# NOTE: no stderr output in the success path — re-verify.sh merges stderr into
# the go-test-json stream (2>&1) and non-JSON lines break its parser.

# --- 1. PinMatchesTag (contract) ---------------------------------------------
# Dereference the (annotated) tag to its commit via the GitHub API.
TAG_OBJ="$(curl -sL "$API/git/ref/tags/$TAG" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d["object"]["sha"]+":"+d["object"]["type"])' 2>/dev/null)" \
  || fail "tag $TAG not found via API"
TAG_SHA="${TAG_OBJ%%:*}"; TAG_TYPE="${TAG_OBJ##*:}"
if [ "$TAG_TYPE" = "tag" ]; then
  COMMIT="$(curl -sL "$API/git/tags/$TAG_SHA" | python3 -c 'import sys,json;print(json.load(sys.stdin)["object"]["sha"])')"
else
  COMMIT="$TAG_SHA"
fi
if [ "$COMMIT" = "$PIN" ]; then
  emit PinMatchesTag pass "pinned $PIN == $TAG dereferenced commit $COMMIT"
else
  emit PinMatchesTag fail "pinned $PIN != $TAG dereferenced commit $COMMIT"
fi

# --- 2. ImageManifestAvailable (contract) ------------------------------------
TOKEN="$(curl -sL "https://ghcr.io/token?scope=repository:ossf/scorecard-action:pull" | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')"
IMG_JSON="$(curl -s -H "Authorization: Bearer $TOKEN" \
  -H "Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json" \
  "https://ghcr.io/v2/ossf/scorecard-action/manifests/$TAG")"
if printf '%s' "$IMG_JSON" | python3 -c '
import sys,json
d=json.load(sys.stdin)
ms=d.get("manifests") or []
# single-arch manifest (v2.5.0) or an index containing linux/amd64
ok=bool(ms and any(m.get("platform",{}).get("architecture")=="amd64" and m.get("platform",{}).get("os")=="linux" for m in ms)) or (not ms and bool(d.get("mediaType")))
sys.exit(0 if ok else 1)'; then
  emit ImageManifestAvailable pass "ghcr.io/ossf/scorecard-action:$TAG manifest reachable, linux/amd64 present"
else
  emit ImageManifestAvailable fail "ghcr.io/ossf/scorecard-action:$TAG missing or no linux/amd64 entry"
fi

# --- 3. PublishResultsEffective (contract, F2) --------------------------------
OPT="$(curl -sL "https://raw.githubusercontent.com/ossf/scorecard-action/$PIN/options/options.go")"
if printf '%s' "$OPT" | grep -q 'env:"INPUT_PUBLISH_RESULTS"' && grep -qE 'publish_results:[[:space:]]*true' "$WF"; then
  emit PublishResultsEffective pass "publish_results: true is honored by $TAG (env tag present); results will be published to api.scorecard.dev"
else
  emit PublishResultsEffective fail "publish_results handling not effective at $PIN (env tag absent or workflow no longer sets it)"
fi

# --- 4. ExcludeInputsIgnored (canary, F1) -------------------------------------
# Current observed (wrong) behavior: exclude_checks / exclude_paths are NOT
# declared inputs of the action -> GitHub drops them with an "Unexpected
# input(s)" warning and the action never reads them. Pass == still present.
curl -sL "https://raw.githubusercontent.com/ossf/scorecard-action/$PIN/action.yaml" -o /tmp/sca-action.yaml.$$
curl -sL "https://raw.githubusercontent.com/ossf/scorecard-action/$PIN/options/env.go" -o /tmp/sca-env.go.$$
UNDECLARED="$(python3 - "$WF" /tmp/sca-action.yaml.$$ /tmp/sca-env.go.$$ <<'EOF'
import re,sys
wf=open(sys.argv[1]).read()
m=re.search(r'uses:\s*ossf/scorecard-action@[0-9a-f]{40}[^\n]*\n {8}with:\n((?: {10}[^\n]+\n)+)',wf)
with_keys=[]
if m:
    for line in m.group(1).splitlines():
        km=re.match(r' {10}([A-Za-z_][A-Za-z0-9_-]*):',line)
        if km: with_keys.append(km.group(1))
action_yaml=open(sys.argv[2]).read()
m2=re.search(r'^inputs:\n((?: {2}[^\n]+\n)+)',action_yaml,re.M)
declared=set(re.findall(r'^ {2}([A-Za-z_][A-Za-z0-9_-]*):$',action_yaml,re.M)) if not m2 else set(re.findall(r'^ {2}([A-Za-z_][A-Za-z0-9_-]*):$',m2.group(1),re.M))
env_go=open(sys.argv[3]).read()
read_env=set(re.findall(r'EnvInput[A-Za-z]+\s*=\s*"([A-Z_0-9]+)"',env_go))
und=[k for k in with_keys if k not in declared and ("INPUT_"+k.upper()) not in read_env]
print("\n".join(und))
EOF
)"
rm -f /tmp/sca-action.yaml.$$ /tmp/sca-env.go.$$
if [ -n "$UNDECLARED" ]; then
  emit ExcludeInputsIgnored pass "undeclared (ignored) with: keys -> $(printf '%s' "$UNDECLARED" | tr '\n' ' ')"
else
  emit ExcludeInputsIgnored fail "all with: keys are now declared action inputs"
fi
exit 0
