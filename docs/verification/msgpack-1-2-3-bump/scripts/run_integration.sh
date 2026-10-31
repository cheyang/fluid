#!/usr/bin/env bash
# Integration layer. Emits ONLY go-test JSON lines on stdout; logs to stderr.
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
exec 2>>"$TOPIC_DIR/results/integration.log"
export TESTJSON_OUT="${TESTJSON_OUT:-$TOPIC_DIR/results/integration.jsonl}"
python3 "$HERE/leak_check.py" >&2
leak_rc=$?
TESTJSON_OUT="${TESTJSON_OUT_PIP:-$TOPIC_DIR/results/integration-pip.jsonl}" bash "$HERE/pip_download_check.sh" >&2
pip_rc=$?
cat "$TESTJSON_OUT" "${TESTJSON_OUT_PIP:-$TOPIC_DIR/results/integration-pip.jsonl}"
[ $leak_rc -eq 0 ] && [ $pip_rc -eq 0 ]
