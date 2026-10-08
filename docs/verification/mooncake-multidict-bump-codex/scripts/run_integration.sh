#!/usr/bin/env bash
# Integration layer for PR #6202 verification.
#
# Simulates the mooncake image build's install step
# (pip install --only-binary=:all: --require-hashes -r requirements.txt)
# against a fake cp312 / linux x86_64 target using pip download, which exercises
# the same wheel selection and --require-hashes verification pip performs in the
# image build (host has no docker / python3.12; this is the closest layer).
#
# Emits go-test-JSON style lines for re-verify.sh:
#   TestPipDownloadRequiresHashesSucceeds   contract: the full pinned, hash-locked
#                                           file resolves and hash-checks.
#   TestHarnessBitesOnCorruptedHashes       negative control: corrupting ALL
#                                           multidict hashes makes pip fail.
#   TestSingleHashLossFallsBackToPureWheel  CANARY (F1): corrupting just the
#                                           cp312-wheel hash does NOT fail the
#                                           build; pip silently falls back to
#                                           the pure-Python py3-none-any wheel,
#                                           because the entry lists hashes for
#                                           every artifact PyPI publishes.
#                                           Flips to FAIL once the entry is
#                                           trimmed to the documented
#                                           one-hash-per-package convention.
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
REQ="$TOPIC_DIR/../../../test/gha-e2e/mooncake/image/requirements.txt"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

emit() { # name, ok(0=pass), note
  if [ "$2" -eq 0 ]; then
    printf '{"Test":"%s","Action":"pass"}\n' "$1"
  else
    printf '{"Test":"%s","Action":"fail"}\n' "$1"
  fi
  [ -n "${3:-}" ] && printf '{"Action":"output","Output":"# %s: %s"}\n' "$1" "$3"
}

PIP_TARGET=(--implementation cp --abi cp312 --python-version 3.12
            --platform manylinux_2_28_x86_64 --platform manylinux_2_17_x86_64
            --platform manylinux2014_x86_64 --only-binary=:all:)

run_pip() { # requirements-file, dest-dir, log
  mkdir -p "$2"
  pip download --no-cache-dir --require-hashes -r "$1" -d "$2" \
      "${PIP_TARGET[@]}" >"$3" 2>&1
}

# --- positive: pristine file must pass, selecting the cp312 C wheel ---------
if run_pip "$REQ" "$WORK/wheels" "$WORK/positive.log"; then
  n=$(ls "$WORK/wheels" | wc -l | tr -d ' ')
  whl=$(ls "$WORK/wheels" | grep '^multidict' || true)
  emit TestPipDownloadRequiresHashesSucceeds 0 "downloaded+hash-checked $n wheels; multidict=$whl"
else
  emit TestPipDownloadRequiresHashesSucceeds 1 "see results/integration-pip-download.log"
fi
cp "$WORK/positive.log" "$TOPIC_DIR/results/integration-pip-download.log"

# --- negative control: corrupt ALL multidict hashes -> pip must fail --------
python3 - "$REQ" "$WORK/requirements-allbad.txt" <<'PY'
import re, sys
txt = open(sys.argv[1]).read()
m = re.search(r"(multidict==[^\n]*\n(?:\s+--hash=sha256:[0-9a-f]+\s*\\?\n)+)", txt)
block = m.group(1)
newblock = re.sub(r'--hash=sha256:([0-9a-f])',
                  lambda mm: '--hash=sha256:' + ('0' if mm.group(1) != '0' else '1'),
                  block)
open(sys.argv[2], 'w').write(txt.replace(block, newblock))
PY
if run_pip "$WORK/requirements-allbad.txt" "$WORK/wheels-bad" "$WORK/negative.log"; then
  emit TestHarnessBitesOnCorruptedHashes 1 "pip ACCEPTED fully corrupted hashes"
else
  if grep -q "DO NOT MATCH THE HASHES" "$WORK/negative.log"; then
    emit TestHarnessBitesOnCorruptedHashes 0 "pip failed with hash-mismatch error"
  else
    emit TestHarnessBitesOnCorruptedHashes 1 "pip failed but not with a hash error"
  fi
fi
cp "$WORK/negative.log" "$TOPIC_DIR/results/integration-corrupted-hash.log"

# --- F1 canary: corrupt ONLY the cp312-wheel hash; observe fallback ----------
sed 's/976fd7689d69ec78d67d31d38d396d8adb562f7e8368279f76aed4aa451fa06d/076fd7689d69ec78d67d31d38d396d8adb562f7e8368279f76aed4aa451fa06d/' \
  "$REQ" > "$WORK/requirements-onebad.txt"
if cmp -s "$REQ" "$WORK/requirements-onebad.txt"; then
  emit TestSingleHashLossFallsBackToPureWheel 1 "cp312-wheel hash not found in file; control invalid"
elif run_pip "$WORK/requirements-onebad.txt" "$WORK/wheels-onebad" "$WORK/onebad.log"; then
  whl=$(ls "$WORK/wheels-onebad" | grep '^multidict' || true)
  if [ "$whl" = "multidict-6.9.1-py3-none-any.whl" ]; then
    emit TestSingleHashLossFallsBackToPureWheel 0 \
      "with the cp312-wheel hash unusable, pip silently installed $whl instead of failing"
  else
    emit TestSingleHashLossFallsBackToPureWheel 1 "pip succeeded with unexpected wheel: $whl"
  fi
else
  emit TestSingleHashLossFallsBackToPureWheel 1 "pip failed the build (fail-loud preserved)"
fi
cp "$WORK/onebad.log" "$TOPIC_DIR/results/integration-single-hash-loss.log" 2>/dev/null || true
