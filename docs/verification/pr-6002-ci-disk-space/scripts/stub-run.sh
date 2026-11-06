#!/usr/bin/env bash
# stub-run.sh — L1 deterministic harness for PR #6002 (fluid-cloudnative/fluid).
#
# Runs .github/scripts/build-all-images.sh from a given repo dir with stubbed
# git/make/kind/docker binaries and asserts the behavior the PR introduces:
#
#   rmi_after_load_pairing   every loaded image is `docker rmi`'d immediately after
#                            its `kind load` (contract for the new behavior)
#   rmi_failure_tolerated    a failing `docker rmi` does NOT abort the script
#                            (set -e + `|| echo` guard) and all loads still happen
#   load_failure_aborts      a failing `kind load` DOES abort the script (set -e
#                            semantics preserved; pre-existing safety)
#   final_cleanup_runs       the trailing cleanup_docker_caches() still runs
#   script_exits_zero        happy path exit code is 0
#
# Output: TAB-separated "name<TAB>pass|fail" lines on stdout (parsed by
# re-verify.sh). Human-readable detail goes to stderr and $RESULTS_DIR.
#
# Usage: stub-run.sh <repo-dir> [results-dir]
set -uo pipefail

REPO_DIR="${1:?usage: stub-run.sh <repo-dir> [results-dir]}"
RESULTS_DIR="${2:-$(pwd)/results/stub-run}"
SCRIPT_REL=".github/scripts/build-all-images.sh"
mkdir -p "$RESULTS_DIR"

if [ ! -f "$REPO_DIR/$SCRIPT_REL" ]; then
  for t in rmi_after_load_pairing rmi_failure_tolerated load_failure_aborts final_cleanup_runs script_exits_zero; do
    printf '%s\tfail\n' "$t"
  done
  echo "stub-run: $SCRIPT_REL not found in $REPO_DIR" >&2
  exit 0
fi

# IMAGE_TAG exactly as the script computes it, with our stub git sha.
VERSION="$(grep '^VERSION := ' "$REPO_DIR/Makefile" | head -1)"; VERSION="${VERSION#VERSION := }"
STUB_SHA="abcdef1"
IMAGE_TAG="${VERSION}-${STUB_SHA}"

new_stub_bin() {
  local d; d="$(mktemp -d)"
  local log="$d/stub.log"; : > "$log"
  cat > "$d/git" <<'S'
#!/bin/bash
if [ "$1" = "rev-parse" ]; then echo abcdef1; exit 0; fi
echo "stub git: unexpected args $*" >&2; exit 1
S
  cat > "$d/make" <<S
#!/bin/bash
echo "make \$*" >> "$log"
exit 0
S
  cat > "$d/kind" <<S
#!/bin/bash
echo "kind \$*" >> "$log"
if [ "\${FAIL_KIND_LOAD:-0}" = "1" ] && [ "\$1" = "load" ]; then exit 1; fi
exit 0
S
  cat > "$d/docker" <<S
#!/bin/bash
echo "docker \$*" >> "$log"
if [ "\$1" = "rmi" ] && [ "\${FAIL_RMI:-0}" = "1" ]; then exit 1; fi
exit 0
S
  chmod +x "$d"/{git,make,kind,docker}
  echo "$d"
}

run_scenario() { # $1=label $2=extra-env -> sets RC, LOG, OUT
  local label="$1" extra="${2:-}"
  local bin; bin="$(new_stub_bin)"
  local out="$RESULTS_DIR/${label}.script.out"
  ( cd "$REPO_DIR" && env -i PATH="$bin:/usr/bin:/bin" HOME="${HOME:-/tmp}" \
      IMG_REPO=testrepo KIND_CLUSTER=test-cluster ${extra} \
      bash "./$SCRIPT_REL" ) > "$out" 2>&1
  RC=$?
  LOG="$bin/stub.log"
  cp "$LOG" "$RESULTS_DIR/${label}.stub.log" 2>/dev/null || true
  rm -rf "$bin" & 2>/dev/null || true  # async cleanup; LOG copied above
  wait 2>/dev/null || true
  LOG="$RESULTS_DIR/${label}.stub.log"
}

verdict() { printf '%s\t%s\n' "$1" "$2"; }

# ---------- S1: happy path ----------
run_scenario s1-happy
S1_RC=$RC; S1_LOG="$LOG"; S1_OUT="$RESULTS_DIR/s1-happy.script.out"

# script_exits_zero
[ "$S1_RC" -eq 0 ] && verdict script_exits_zero pass || verdict script_exits_zero fail

# final_cleanup_runs: cleanup_docker_caches issues `docker image prune -a -f` after the loop
if grep -q '^docker image prune -a -f$' "$S1_LOG"; then verdict final_cleanup_runs pass; else verdict final_cleanup_runs fail; fi

# rmi_after_load_pairing: state machine over the load/rmi subsequence
awk '
  /^kind load docker-image / {
    loads++
    if (pending != "") { bad++; printf("  [pairing] load before pending rmi cleared: %s\n", $4) > "/dev/stderr" }
    pending=$4; next
  }
  /^docker rmi / {
    rmis++
    if (pending == "") { bad++; printf("  [pairing] rmi with no preceding load: %s\n", $3) > "/dev/stderr" }
    else if (pending != $3) { bad++; printf("  [pairing] rmi %s does not match pending load %s\n", $3, pending) > "/dev/stderr" }
    pending=""; next
  }
  END {
    if (pending != "") { bad++; printf("  [pairing] trailing load never rmi\047d: %s\n", pending) > "/dev/stderr" }
    if (loads < 10) { bad++; printf("  [pairing] only %d loads, expected >=10\n", loads) > "/dev/stderr" }
    if (loads != rmis) { bad++; printf("  [pairing] loads=%d rmis=%d\n", loads, rmis) > "/dev/stderr" }
    printf("  [pairing] loads=%d rmis=%d violations=%d\n", loads, rmis, bad) > "/dev/stderr"
    exit (bad==0 ? 0 : 1)
  }
' "$S1_LOG" 2>"$RESULTS_DIR/s1-happy.pairing.txt"
if [ $? -eq 0 ]; then verdict rmi_after_load_pairing pass; else verdict rmi_after_load_pairing fail; fi

# ---------- S2: docker rmi always fails -> script must survive, all loads happen ----------
run_scenario s2-rmi-fails "FAIL_RMI=1"
S2_RC=$RC; S2_LOG="$LOG"; S2_OUT="$RESULTS_DIR/s2-rmi-fails.script.out"
s2_loads="$(grep -c '^kind load docker-image ' "$S2_LOG" || true)"
s2_warns="$(grep -c 'Warning: Failed to remove image' "$S2_OUT" || true)"
if [ "$S2_RC" -eq 0 ] && [ "$s2_loads" -ge 10 ] && [ "$s2_warns" -ge 10 ]; then
  verdict rmi_failure_tolerated pass
else
  verdict rmi_failure_tolerated fail
fi
echo "  [rmi-fails] rc=$S2_RC loads=$s2_loads warnings=$s2_warns" > "$RESULTS_DIR/s2-rmi-fails.txt"

# ---------- S3: kind load fails -> set -e must abort, nothing rmi'd ----------
run_scenario s3-load-fails "FAIL_KIND_LOAD=1"
S3_RC=$RC; S3_LOG="$LOG"
s3_loads="$(grep -c '^kind load docker-image ' "$S3_LOG" || true)"
s3_rmis="$(grep -c '^docker rmi ' "$S3_LOG" || true)"
if [ "$S3_RC" -ne 0 ] && [ "$s3_rmis" -eq 0 ]; then
  verdict load_failure_aborts pass
else
  verdict load_failure_aborts fail
fi
echo "  [load-fails] rc=$S3_RC loads=$s3_loads rmis=$s3_rmis" > "$RESULTS_DIR/s3-load-fails.txt"
exit 0
