#!/usr/bin/env bash
# L2 (integration) layer — prove the mooncake image's pip install contract holds
# for the PR-head lockfile, without needing docker or python 3.12 on this host.
#
# The Dockerfile RUN step is:
#   pip install --no-cache-dir --only-binary=:all: --require-hashes -r requirements.txt
# inside python:3.12.13-slim on linux/amd64. We reproduce its resolution with
# pip's cross-environment flags (--python-version 3.12 --platform ... --abi cp312)
# so every wheel pip would fetch in the image build is downloaded and hash-checked
# here, then install the changed package (certifi) for real and inspect its CA bundle.
#
# Also runs the harness-bites self-test: the same install with a tampered hash
# MUST fail, proving the --require-hashes contract is actually enforced by this
# harness (a green result is then meaningful).
#
# Emits "<claim>\t<pass|fail>" lines for re-verify.sh; details to stderr and
# $RESULTS_DIR (default: results/ next to this script's parent).
#
# Requires: python3 with venv, network access to PyPI. A broken host pip
# (seen on this reviewer box: pip 23.3.1 vendored-urllib3 TypeError) is worked
# around by bootstrapping a current pip via get-pip.py, which uses stdlib urllib.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOPIC_DIR="$(dirname "$HERE")"
RESULTS_DIR="${RESULTS_DIR:-$TOPIC_DIR/results}"
REQ="${REQ:-$TOPIC_DIR/../../../test/gha-e2e/mooncake/image/requirements.txt}"
REQ="$(cd "$(dirname "$REQ")" && pwd)/$(basename "$REQ")"
mkdir -p "$RESULTS_DIR"

PY_TARGETS=(--python-version 3.12 --platform manylinux_2_28_x86_64 --platform manylinux2014_x86_64 --implementation cp --abi cp312)
VENV="$(mktemp -d)/venv"
WORK="$(mktemp -d)"
trap 'rm -rf "$(dirname "$VENV")" "$WORK"' EXIT

bootstrap_pip() {
  python3 -m venv "$VENV" >/dev/null 2>&1
  # quick health probe; fall back to get-pip.py if the venv pip is broken
  if ! "$VENV/bin/pip" download --no-deps --only-binary=:all: --disable-pip-version-check \
        -d "$WORK/probe" idna==3.19 >/dev/null 2>&1; then
    echo "  venv pip unhealthy; bootstrapping via get-pip.py" >&2
    curl -sSLo "$WORK/get-pip.py" https://bootstrap.pypa.io/get-pip.py || return 1
    "$VENV/bin/python" "$WORK/get-pip.py" >/dev/null 2>&1 || return 1
  fi
  "$VENV/bin/pip" --version >&2
}

emit() { printf '%s\t%s\n' "$1" "$2"; }

# ---------------------------------------------------------------- L2-FULL-RESOLVE
echo "== L2: full lockfile resolution for python:3.12.13-slim (linux/amd64)" >&2
if bootstrap_pip; then
  if "$VENV/bin/pip" download -r "$REQ" --only-binary=:all: --require-hashes --no-deps \
       --disable-pip-version-check "${PY_TARGETS[@]}" -d "$WORK/dl" \
       >"$RESULTS_DIR/l2-full-resolve.log" 2>&1; then
    n="$(ls "$WORK/dl" | wc -l)"
    echo "  resolved and hash-verified $n/17 wheels" >&2
    [ "$n" -eq 17 ] && emit L2-FULL-RESOLVE pass || emit L2-FULL-RESOLVE fail
  else
    echo "  pip download FAILED (see l2-full-resolve.log)" >&2
    emit L2-FULL-RESOLVE fail
  fi
else
  echo "  could not bootstrap a working pip" >&2
  emit L2-FULL-RESOLVE fail
fi

# -------------------------------------------------------------- L2-CERTIFI-INSTALL
echo "== L2: install the changed certifi stanza exactly as the Dockerfile does" >&2
STANZA="$WORK/certifi-stanza.txt"
sed -n '/^certifi==/,/# via /p' "$REQ" > "$STANZA"
if "$VENV/bin/pip" install --require-hashes --only-binary=:all: --no-deps --no-cache-dir \
     --disable-pip-version-check -r "$STANZA" >"$RESULTS_DIR/l2-certifi-install.log" 2>&1 \
 && "$VENV/bin/python" -I -c "
import certifi
pem = open(certifi.where(), 'rb').read()
assert len(pem) > 100000, 'CA bundle too small'
n = pem.count(b'-----BEGIN CERTIFICATE-----')
print('  certifi', certifi.__version__ if hasattr(certifi, '__version__') else '', 'bundle certs:', n)
assert n >= 100, 'CA bundle suspiciously small'
" >>"$RESULTS_DIR/l2-certifi-install.log" 2>&1; then
  emit L2-CERTIFI-INSTALL pass
else
  echo "  certifi install/inspect FAILED (see l2-certifi-install.log)" >&2
  emit L2-CERTIFI-INSTALL fail
fi

# ------------------------------------------------------------------ harness-bites
# NOTE 1: --no-cache-dir is essential here (and matches the Dockerfile): with a
# warm pip HTTP cache, a cached wheel can bypass the requirement-hash comparison.
# NOTE 2: the tampered hash is derived from the stanza itself (zero out the last
# pinned hash), with an assertion that the substitution actually applied — an
# earlier version hand-typed the hash literal, typo'd it, silently tampered
# nothing, and made this check meaningless.
echo "== L2: harness-bites self-test (tampered hash must be rejected)" >&2
TAMPER="$WORK/certifi-tampered.txt"
if ! python3 - "$STANZA" "$TAMPER" <<'PYEOF'
import re, sys
src, dst = sys.argv[1], sys.argv[2]
txt = open(src).read()
# zero out the last pinned hash of the certifi stanza (the one preceding the
# "# via" comment — the hash pip checks the downloaded wheel against)
tampered, n = re.subn(r'--hash=sha256:[0-9a-f]{64}(?=\s*\n\s*# via)',
                      '--hash=sha256:' + '0' * 64, txt, count=1)
if n != 1:
    sys.exit('tamper substitution did not apply — stanza shape changed')
open(dst, 'w').write(tampered)
PYEOF
then
  echo "  could not build tampered stanza (stanza shape changed?) — harness needs update" >&2
  emit L2-HARNESS-BITES fail
  echo "results in $RESULTS_DIR" >&2
  exit 1
fi
if "$VENV/bin/pip" install --require-hashes --only-binary=:all: --no-deps --no-cache-dir \
     --disable-pip-version-check --force-reinstall -r "$TAMPER" \
     >"$RESULTS_DIR/l2-harness-bites.log" 2>&1; then
  echo "  TAMPERED HASH WAS ACCEPTED — harness does not bite" >&2
  emit L2-HARNESS-BITES fail
else
  echo "  tampered hash rejected as expected" >&2
  emit L2-HARNESS-BITES pass
fi

echo "results in $RESULTS_DIR" >&2
