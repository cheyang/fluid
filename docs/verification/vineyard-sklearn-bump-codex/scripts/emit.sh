# emit.sh — sourced by layer runners. Emits ONE go-test-json event line per check
# to stdout (all real logs go to per-check files under $VERIFY_RESULTS).
VERIFY_RESULTS="${VERIFY_RESULTS:-docs/verification/vineyard-sklearn-bump-codex/results}"
mkdir -p "$VERIFY_RESULTS"
emit() { # <TestName> <pass|fail> <logfile>
  python3 - "$1" "$2" "$3" <<'PY'
import json, sys
name, status, log = sys.argv[1:4]
try:
    with open(log, errors="replace") as fh:
        tail = "".join(fh.readlines()[-8:])
except Exception:
    tail = ""
# compact separators: re-verify.sh greps for the exact string '"Action":"pass"'
print(json.dumps({"Action": status, "Test": name, "Output": tail[-1500:]}, separators=(",", ":")))
PY
}
