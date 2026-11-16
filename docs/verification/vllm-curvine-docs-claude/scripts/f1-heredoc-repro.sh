#!/usr/bin/env bash
# f1-heredoc-repro.sh — F1 (PR #6253), mechanism 2: the delegated CacheRuntimeClass
# heredoc in docs/en/samples/cacheruntime/curvine_cache_runtime.md is an UNQUOTED
# `cat<<EOF`, so a reader with CURVINE_HOME unset gets `${CURVINE_HOME}` expanded
# to the empty string before the file hits disk (CURVINE_CONF_DIR="/conf", …).
#
# Contract polarity: PASS = the referenced doc's heredoc is expansion-safe
# (quoted `cat<<"EOF"` or no shell expansions in the body). FAIL = reproduction
# (the mangling occurs). Expected to flip to PASS once #6252 (or equivalent) merges.
#
# Usage: f1-heredoc-repro.sh [<git-ref>]   (default: working tree)
set -uo pipefail

REF="${1:-WORKTREE}"
DOC="docs/en/samples/cacheruntime/curvine_cache_runtime.md"
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

if [ "$REF" = "WORKTREE" ]; then
  SRC="$ROOT/$DOC"
else
  TMP="$(mktemp)"
  git -C "$ROOT" show "$REF:$DOC" >"$TMP" || { echo "F1_heredoc	repro-ref-missing"; exit 2; }
  SRC="$TMP"
fi

# Extract the cacheruntimeclass.yaml heredoc block (quoted or unquoted delimiter):
# from `cat<<EOF >cacheruntimeclass.yaml` / `cat<<'EOF' >cacheruntimeclass.yaml` to the closing EOF
BLOCK="$(awk '/cat<<.?EOF.? *>cacheruntimeclass\.yaml/{f=1;next} f&&/^EOF\$/{exit} f{print}' "$SRC")"
[ -n "$BLOCK" ] || { echo "F1_heredoc	block-not-found"; exit 2; }

# Does the delimiter appear quoted (cat<<"EOF" or cat<<'EOF')? Then no expansion happens: safe.
if grep -q 'cat<<["'"'"']EOF["'"'"'] *>cacheruntimeclass\.yaml' "$SRC"; then
  echo "F1_heredoc	pass	quoted delimiter — no expansion"
  exit 0
fi

# Unquoted heredoc: check for shell expansions in the body.
if ! printf '%s\n' "$BLOCK" | grep -q '[`${]'; then
  echo "F1_heredoc	pass	no shell expansions in body"
  exit 0
fi

# Reproduce the mangling: expand with CURVINE_HOME (and friends) unset.
if command -v envsubst >/dev/null 2>&1; then
  MANGLED="$(printf '%s\n' "$BLOCK" | CURVINE_HOME= envsubst)"
else
  # fallback: ${CURVINE_HOME} -> empty
  MANGLED="$(printf '%s\n' "$BLOCK" | sed -e 's/\${CURVINE_HOME}//g')"
fi
echo "--- reader's resulting file (excerpt) ---"
printf '%s\n' "$MANGLED" | grep -n 'CURVINE_CONF_DIR\|CURVINE_DATA_DIR\|CURVINE_LOG_DIR' | sed 's/^/  /'
if printf '%s\n' "$MANGLED" | grep -q 'CURVINE_CONF_DIR="/conf"'; then
  echo "F1_heredoc	fail	unquoted heredoc: CURVINE_HOME expands empty, CURVINE_CONF_DIR mangled to \"/conf\""
  exit 1
fi
echo "F1_heredoc	fail	heredoc body contains shell expansions"
exit 1
