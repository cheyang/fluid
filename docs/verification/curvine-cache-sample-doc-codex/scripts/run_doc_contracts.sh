#!/usr/bin/env bash
# run_doc_contracts.sh <ref> — unit layer for the Curvine CacheRuntime sample doc.
#
# Extracts every `cat<<...` heredoc block from the doc at <ref> (en + zh),
# executes it the way a reader would (env -i bash, empty environment), and
# checks the generated YAML against the contracts the PR relies on.
#
# Output: go-test-JSON lines on stdout ({"Test":...,"Action":"pass|fail"}),
# one per contract per language, so the stock review-finding-verifier
# re-verify.sh (kind: gotest) can consume it unmodified.
# Diagnostics go to stderr. Generated YAML is kept under $DOC_VERIFY_ARTIFACTS
# (if set) as raw evidence.
#
# If the doc shape changes (block count, renamed targets) the script emits a
# build-fail marker so re-verify reports HARNESS-UPDATE instead of a bogus
# pass/fail.
set -uo pipefail

REF="${1:-HEAD}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

DOC_EN="docs/en/samples/cacheruntime/curvine_cache_runtime.md"
DOC_ZH="docs/zh/samples/cacheruntime/curvine_cache_runtime.md"
EXPECTED_BLOCKS=10

emit() { # $1 pass|fail  $2 test name  $3 detail
  local action="$1" name="$2" detail="${3:-}"
  detail="${detail//\\/\\\\}"; detail="${detail//\"/\\\"}"; detail="${detail//$'\n'/ }"
  printf '{"Test":"%s","Action":"%s","Output":"%s"}\n' "$name" "$action" "$detail"
}
emit_buildfail() { # $1 package-ish label  $2 detail
  local detail="${2//\"/\\\"}"
  printf '{"Action":"build-fail","Package":"docharness","Output":"%s: %s"}\n' "$1" "$detail"
}

check_lang() { # $1 lang tag (EN|ZH)  $2 doc path
  local lg="$1" docpath="$2"
  local tmp; tmp="$(mktemp -d)"
  if ! git show "$REF:$docpath" > "$tmp/doc.md" 2>"$tmp/show.err"; then
    emit_buildfail "$lg" "cannot read $docpath at $REF: $(cat "$tmp/show.err")"
    rm -rf "$tmp"; return 0
  fi
  if ! python3 "$HERE/extract_heredocs.py" "$tmp/doc.md" "$tmp/blocks" 2>"$tmp/extract.err"; then
    emit_buildfail "$lg" "extractor failed: $(cat "$tmp/extract.err")"
    rm -rf "$tmp"; return 0
  fi
  local nblocks; nblocks="$(python3 -c 'import json,sys;print(len(json.load(open(sys.argv[1]))))' "$tmp/blocks/blocks.json")"
  if [ "$nblocks" != "$EXPECTED_BLOCKS" ]; then
    emit_buildfail "$lg" "expected $EXPECTED_BLOCKS heredoc blocks, found $nblocks"
    rm -rf "$tmp"; return 0
  fi

  # Execute each block exactly as written, in an empty environment.
  local rundir="$tmp/run"; mkdir -p "$rundir"
  python3 - "$tmp/blocks/blocks.json" > "$tmp/order.txt" <<'PY'
import json,sys
for b in json.load(open(sys.argv[1])):
    print("%s\t%s" % (b["target"], b["script"]))
PY
  while IFS=$'\t' read -r target script; do
    ( cd "$rundir" && env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=/nonexistent \
        bash "$script" >"$tmp/$(basename "$script").stdout" 2>"$tmp/$(basename "$script").stderr" )
  done < "$tmp/order.txt"

  # keep raw evidence
  if [ -n "${DOC_VERIFY_ARTIFACTS:-}" ]; then
    mkdir -p "$DOC_VERIFY_ARTIFACTS/$lg"
    cp "$rundir"/* "$DOC_VERIFY_ARTIFACTS/$lg/" 2>/dev/null || true
    cp "$tmp"/*.stderr "$DOC_VERIFY_ARTIFACTS/$lg/" 2>/dev/null || true
  fi

  local f
  # --- contract: CacheRuntimeClass block preserves pod-side shell variables ---
  f="$rundir/cacheruntimeclass.yaml"
  if [ ! -f "$f" ]; then
    emit_buildfail "$lg" "cacheruntimeclass.yaml block missing"
  elif grep -qF 'CURVINE_CONF_DIR="${CURVINE_HOME}/conf"' "$f" \
    && grep -qF '$FLUID_DATALOAD_DATA_PATH' "$f" \
    && grep -qF 'target_path="${CURVINE_TARGET_PATH:-${FLUID_RUNTIME_MOUNT_PATH:-}}"' "$f"; then
    emit pass "DocContract${lg}/CacheRuntimeClassVars" "pod-side \$ vars preserved literally"
  else
    local mangled; mangled="$(grep -o 'CURVINE_CONF_DIR=[^ ]*' "$f" | head -1)"
    emit fail "DocContract${lg}/CacheRuntimeClassVars" "pod-side \$ vars expanded/mangled by local shell (generated: $mangled)"
  fi

  # --- contract: read job keeps $(cat ...) for the in-cluster shell ---
  for pair in "read_job.yaml:ReadJobCmdSubst" "read_ref_job.yaml:ReadRefJobCmdSubst"; do
    f="$rundir/${pair%%:*}"; local tn="${pair##*:}"
    if [ ! -f "$f" ]; then
      emit_buildfail "$lg" "${pair%%:*} block missing"
    elif grep -qF 'set -ex; test -n "$(cat /data/minio/bar)"' "$f"; then
      emit pass "DocContract${lg}/$tn" '$(cat /data/minio/bar) preserved for pod shell'
    else
      emit fail "DocContract${lg}/$tn" '$(cat ...) was expanded by the local shell at doc-follow time'
    fi
  done

  # --- contract: bucket job (deliberately UNquoted) yields single-$ env refs ---
  f="$rundir/minio_create_bucket.yaml"
  if [ ! -f "$f" ]; then
    emit_buildfail "$lg" "minio_create_bucket.yaml block missing"
  elif grep -qF '$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD' "$f" \
    && ! grep -qF '\$MINIO_ROOT_USER' "$f"; then
    emit pass "DocContract${lg}/BucketJobEscapeContract" '\$ collapsed to $ for the pod shell'
  else
    emit fail "DocContract${lg}/BucketJobEscapeContract" 'bucket job heredoc no longer yields bare $MINIO_ROOT_USER (was it quoted?)'
  fi

  # --- contract: every generated manifest is valid YAML ---
  local bad=""
  for y in "$rundir"/*.yaml; do
    python3 -c 'import yaml,sys; list(yaml.safe_load_all(open(sys.argv[1])))' "$y" 2>"$tmp/yaml.err" \
      || bad="$bad $(basename "$y"):$(head -1 "$tmp/yaml.err")"
  done
  if [ -z "$bad" ]; then
    emit pass "DocContract${lg}/GeneratedYAMLValid" "all $EXPECTED_BLOCKS generated files parse as YAML"
  else
    emit fail "DocContract${lg}/GeneratedYAMLValid" "unparseable YAML:$bad"
  fi

  # --- contract: curvine image references carry the v-prefix tag ---
  local badimg
  badimg="$(grep -nE 'image:\s*curvine/curvine:' "$tmp/doc.md" | grep -v 'curvine/curvine:v' || true)"
  if [ -z "$badimg" ]; then
    emit pass "DocContract${lg}/ImageTagVPrefix" "all curvine/curvine refs are v-prefixed"
  else
    emit fail "DocContract${lg}/ImageTagVPrefix" "non-v-prefixed curvine tag(s): $(echo "$badimg" | tr '\n' ';')"
  fi

  rm -rf "$tmp"
}

check_lang EN "$DOC_EN"
check_lang ZH "$DOC_ZH"
