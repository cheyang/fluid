#!/usr/bin/env bash
# Unit layer entry point. Emits ONLY go-test JSON lines on stdout (re-verify.sh
# parses them); human logs go to stderr.
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
exec 2>>"$TOPIC_DIR/results/unit.log"
export TESTJSON_OUT="${TESTJSON_OUT:-$TOPIC_DIR/results/unit.jsonl}"
python3 "$HERE/check_hashes.py" >&2
cat "$TESTJSON_OUT"
