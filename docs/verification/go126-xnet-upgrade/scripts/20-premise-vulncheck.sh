#!/usr/bin/env bash
# P0 premise check: govulncheck (symbol-level, reachable) on the BASE branch
# (go1.25.12, x/net v0.58.0) and on the HEAD (go1.26.10, x/net v0.62.0).
# Polarity: contract. Expected on base: reachable stdlib + x/net vulns
# (incl. GO-2026-6617 and GO-2026-6809). Expected on head: 0 reachable
# stdlib/x/net vulns (only the 4 k8s.io/kubernetes ones remain, out of scope).
set -u
BASE_WT="${1:?usage: $0 <base-worktree> <head-worktree>}"
HEAD_WT="${2:?usage: $0 <base-worktree> <head-worktree>}"
RESULTS="${RESULTS_DIR:-$(dirname "$0")/../results}"
command -v govulncheck >/dev/null || { echo "govulncheck not installed (go install golang.org/x/vuln/cmd/govulncheck@latest)"; exit 2; }

( cd "$BASE_WT" && GOTOOLCHAIN=go1.25.12 govulncheck ./... ) > "$RESULTS/vulncheck-base.log" 2>&1
( cd "$HEAD_WT" && GOTOOLCHAIN=go1.26.10 govulncheck ./... ) > "$RESULTS/vulncheck-head.log" 2>&1

summarize() {
  local log="$1" tag="$2"
  local affected nonk8s
  affected=$(grep -c "^Vulnerability #" "$log")
  nonk8s=$(awk '/^Vulnerability #/{v=1} /Module: k8s.io\/kubernetes/{if(v)v=0} /More info/{if(v)c++} {if($0 ~ /^Vulnerability #/) v=1}' "$log" 2>/dev/null; true)
  echo "[$tag] reachable vulnerabilities: $affected"
  grep -m1 "Your code is affected by" "$log" || true
}
summarize "$RESULTS/vulncheck-base.log" "base"
summarize "$RESULTS/vulncheck-head.log" "head"
echo "--- key IDs ---"
grep -o "GO-[0-9-]*" "$RESULTS/vulncheck-base.log" | sort -u | head -30
