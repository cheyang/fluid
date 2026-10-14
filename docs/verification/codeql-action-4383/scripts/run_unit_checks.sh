#!/usr/bin/env bash
# Unit layer for the codeql-action-4383 verification harness (PR #6209).
#
# Runs four deterministic checks against the working tree / current HEAD and
# emits go-test-JSON lines on stdout so scripts/re-verify.sh (kind: gotest)
# can parse the results. All diagnostics go to stderr.
#
# Checks:
#   TestPinSHAsResolveToExactReleaseTags
#     every `uses: github/codeql-action/*@<sha>` in the repo's workflows pins
#     the peeled commit of a real refs/tags/v* release tag of
#     https://github.com/github/codeql-action.git (supply-chain provenance).
#   TestPinCommentMatchesResolvedTag
#     the trailing `# vX.Y.Z` comment on each pin equals the tag the pinned
#     SHA actually resolves to (F1: stale comments).
#   TestPRDiffTouchesOnlyCodeqlActionPins
#     the base..HEAD diff of the two workflow files changes only
#     `uses: github/codeql-action/...` lines, and no file outside those two
#     workflows (plus this harness) is touched by the PR.
#   TestWorkflowFilesParseAsYaml
#     both touched workflow files are syntactically valid YAML.
set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
WORKFLOWS=(.github/workflows/codeql-analysis.yml .github/workflows/sonarcloud.yaml)
UPSTREAM="https://github.com/github/codeql-action.git"
RESULTS_DIR="$(dirname "$0")/../results"
mkdir -p "$RESULTS_DIR"

emit() { # $1=Action(pass|fail) $2=Test name -- one go-test-JSON line on stdout
  printf '{"Action":"%s","Test":"%s","Package":"verify/codeql-action-4383"}\n' "$1" "$2"
}
fail() { emit fail "$1"; shift; echo "[FAIL] $*" >&2; }
pass() { emit pass "$1"; shift; echo "[pass] $*" >&2; }

# ---- resolve pin SHA -> release tag via peeled ls-remote refs (no clone needed)
TAGMAP_FILE="$RESULTS_DIR/codeql-action-tags.tsv"
if ! git ls-remote --tags "$UPSTREAM" >"$TAGMAP_FILE.raw" 2>"$RESULTS_DIR/ls-remote.err"; then
  echo "[FATAL] git ls-remote against $UPSTREAM failed; see ls-remote.err" >&2
  exit 1
fi
# peeled entries: "<commit-sha>\trefs/tags/vX.Y.Z^{}"  ->  sha -> vX.Y.Z
# Only full-semver release tags (vX.Y.Z): the repo also carries moving major tags
# (v4 -> latest release) whose peeled sha collides with the release tag; those
# must not be reported as "the" release the pin resolves to.
awk -F'\t' '$2 ~ /\^\{\}$/ {
  tag=$2; sub(/\^\{\}/,"",tag); sub(/refs\/tags\//,"",tag);
  if (tag ~ /^v[0-9]+\.[0-9]+\.[0-9]+$/) print $1 "\t" tag;
}' "$TAGMAP_FILE.raw" | sort -u >"$TAGMAP_FILE"

resolve_tag() { # $1=sha -> tag name or empty
  awk -F'\t' -v s="$1" '$1==s {print $2; exit}' "$TAGMAP_FILE"
}

# ---- collect pins: file:line\tsubaction\tsha\tcomment
PINS_FILE="$RESULTS_DIR/pins.tsv"
: >"$PINS_FILE"
for f in "${WORKFLOWS[@]}"; do
  grep -nE '^[[:space:]]*uses:[[:space:]]*github/codeql-action/[^@[:space:]]+@[0-9a-f]{40,}' \
    "$REPO_ROOT/$f" | while IFS= read -r line; do
    lineno="${line%%:*}"; rest="${line#*:}"
    spec="$(echo "$rest" | sed -E 's/^[[:space:]]*uses:[[:space:]]*//; s/[[:space:]].*//')"
    comment="$(echo "$rest" | grep -oE '#[[:space:]]*[^[:space:]]+' | head -1 | tr -d '# ' || true)"
    sub="${spec%@*}"; sha="${spec##*@}"
    printf '%s\t%s\t%s\t%s\n' "$f:$lineno" "$sub" "$sha" "${comment:-}" >>"$PINS_FILE"
  done
done

# ==== Check 1: every pin is the peeled commit of a release tag ====
c1_ok=1
while IFS=$'\t' read -r loc sub sha comment; do
  tag="$(resolve_tag "$sha")"
  if [ -z "$tag" ]; then
    echo "  $loc $sub@$sha does NOT resolve to any refs/tags/v* release tag" >&2
    c1_ok=0
  else
    echo "  $loc $sub -> $tag ($sha)" >&2
  fi
done <"$PINS_FILE"
[ "$(wc -l <"$PINS_FILE")" -ge 1 ] || { echo "  no pins found (workflows changed shape?)" >&2; c1_ok=0; }
if [ "$c1_ok" -eq 1 ]; then pass TestPinSHAsResolveToExactReleaseTags "all pins resolve to exact release-tag commits"; \
else fail TestPinSHAsResolveToExactReleaseTags "a pin is not a codeql-action release-tag commit"; fi

# ==== Check 2 (F1): the '# vX' comment matches the resolved tag ====
c2_ok=1
while IFS=$'\t' read -r loc sub sha comment; do
  tag="$(resolve_tag "$sha")"
  norm="${comment#v}"   # strip optional leading v so 'v4.38.3' and '4.38.3' both compare equal
  if [ -z "$comment" ]; then
    echo "  $loc $sub@$sha has no '# vX' version comment" >&2; c2_ok=0
  elif [ "v$norm" != "$tag" ]; then
    echo "  $loc comment says $comment but pin resolves to $tag" >&2; c2_ok=0
  fi
done <"$PINS_FILE"
if [ "$c2_ok" -eq 1 ]; then pass TestPinCommentMatchesResolvedTag "all pin comments match resolved tags"; \
else fail TestPinCommentMatchesResolvedTag "stale pin comments (F1)"; fi

# ==== Check 3: PR diff shape — only codeql-action pin lines changed ====
MB="$(git merge-base origin/master HEAD 2>/dev/null || true)"
c3_ok=1
if [ -z "$MB" ]; then
  echo "  cannot compute merge-base with origin/master" >&2; c3_ok=0
else
  # no file outside the two workflows (+ harness docs) may be touched
  while IFS= read -r changed; do
    case "$changed" in
      .github/workflows/codeql-analysis.yml|.github/workflows/sonarcloud.yaml) ;;
      docs/verification/*) ;;
      *) echo "  unexpected changed file: $changed" >&2; c3_ok=0;;
    esac
  done < <(git diff --name-only "$MB" HEAD)
  # within the two workflows, every +/- line must be a codeql-action pin line
  # (skip file headers: "+++ b/..." / "--- a/...")
  while IFS= read -r dl; do
    case "$dl" in
      +++*|---*) ;;
      +*|-*|\\*)
        content="${dl:1}"
        if [ -n "$content" ] && ! echo "$content" | grep -qE '^[[:space:]]*uses:[[:space:]]*github/codeql-action/'; then
          echo "  unexpected diff line: $dl" >&2; c3_ok=0
        fi ;;
    esac
  done < <(git diff "$MB" HEAD -- "${WORKFLOWS[@]}")
fi
if [ "$c3_ok" -eq 1 ]; then pass TestPRDiffTouchesOnlyCodeqlActionPins "diff is exactly codeql-action pin updates"; \
else fail TestPRDiffTouchesOnlyCodeqlActionPins "diff contains more than pin updates"; fi

# ==== Check 4: YAML validity ====
c4_ok=1
for f in "${WORKFLOWS[@]}"; do
  if ! python3 -I -c "import yaml,sys; yaml.safe_load(open(sys.argv[1]))" "$REPO_ROOT/$f" 2>/dev/null; then
    echo "  $f failed yaml.safe_load" >&2; c4_ok=0
  fi
done
if [ "$c4_ok" -eq 1 ]; then pass TestWorkflowFilesParseAsYaml "both workflows parse as YAML"; \
else fail TestWorkflowFilesParseAsYaml "a workflow file is invalid YAML"; fi

# package-level summary lines (harmless for the parser, faithful to go test -json)
printf '{"Action":"pass","Package":"verify/codeql-action-4383","Elapsed":0}\n'
