#!/usr/bin/env bash
# C5/P0: govulncheck symbol-level scan.
#   check-govulncheck.sh head  -> contract: zero reachable vulns whose
#                                 "Found in" module is the Go standard library
#                                 or golang.org/x/net (k8s.io/kubernetes
#                                 findings are a documented out-of-scope
#                                 exception, tracked separately).
#   check-govulncheck.sh base  -> premise canary (runs against BASE ref):
#                                 exit 0 only when at least one reachable
#                                 stdlib or x/net finding EXISTS, i.e. the
#                                 problem the PR claims to fix is real.
# Needs: govulncheck in PATH (go install golang.org/x/vuln/cmd/govulncheck@v1.9.0)
# and network access to vuln.go.dev.
set -euo pipefail
MODE="${1:?usage: check-govulncheck.sh head|base}"
ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"
TC="$(awk '/^toolchain /{print $2; exit}' go.mod)"
GV="$(command -v govulncheck || true)"
[ -n "$GV" ] || GV="$(go env GOPATH)/bin/govulncheck"
[ -x "$GV" ] || { echo "HARNESS-ERROR: govulncheck not found (go install golang.org/x/vuln/cmd/govulncheck@v1.9.0)"; exit 2; }
echo "using GOTOOLCHAIN=$TC govulncheck=$GV (mode=$MODE)"
OUT="$(mktemp)"
set +e
GOTOOLCHAIN="$TC" "$GV" -mode=source ./... >"$OUT" 2>&1
GV_EXIT=$?
set -e
# Never pass vacuously: the scan must have produced its symbol-level section.
grep -q "=== Symbol Results ===" "$OUT" || { echo "HARNESS-ERROR: govulncheck produced no Symbol Results (exit $GV_EXIT):"; tail -20 "$OUT"; exit 2; }
# reachable findings grouped by module ("Found in: <mod>@<ver>")
REACHABLE=$(awk '/^=== Symbol Results ===/{s=1} s&&/Found in:/{print $3}' "$OUT" | sed 's/@.*//' | sort -u)
STDLIB_OR_XNET=$(echo "$REACHABLE" | grep -vE '^k8s.io/kubernetes$' | grep -vE '^$' || true)
echo "reachable modules: $(echo "$REACHABLE" | tr '\n' ' ')"
case "$MODE" in
  head)
    if [ -n "$STDLIB_OR_XNET" ]; then
      echo "FAIL: reachable stdlib/x-net vulnerabilities remain:"; echo "$STDLIB_OR_XNET"
      grep -B2 -A6 "Found in: .*go1\.\|Found in: golang.org/x/net" "$OUT" | head -40; exit 1
    fi
    echo "PASS: no reachable stdlib/x-net findings (out-of-scope k8s.io/kubernetes findings may remain)";;
  base)
    if [ -z "$STDLIB_OR_XNET" ]; then
      echo "CANARY-FLIP: premise no longer reproduces on this ref (no reachable stdlib/x-net findings)"; exit 1
    fi
    echo "PASS (canary): premise reproduces — reachable stdlib/x-net findings present";;
esac
