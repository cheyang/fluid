#!/usr/bin/env bash
# verify_msgpack_lock.sh — contract harness for PR fluid-cloudnative/fluid#6226
# (dependabot bump msgpack 1.2.2 -> 1.2.3 in test/gha-e2e/mooncake/image/requirements.txt).
#
# Verifies, against a hash-locked requirements.txt:
#   C1 (unit):        the msgpack==<ver> entry's --hash set matches PyPI's published
#                     sha256 digests for <ver> exactly (no extra/tampered, none missing),
#                     and the digests needed by the image's platform are present.
#   C2 (integration): pip's real --require-hashes path accepts the entry for the image's
#                     platform (python 3.12, manylinux x86_64, wheels only) — the same
#                     constraint set the Dockerfile imposes.
#
# Both are CONTRACT tests: they assert the intended correct lock and FAIL on a
# tampered / inconsistent one (see results/harness-bites.txt for the red-run proof).
#
# Usage:
#   verify_msgpack_lock.sh <requirements.txt> [results-dir]
#
# Env:
#   PYPI_JSON   optional path/url of a cached https://pypi.org/pypi/msgpack/<ver>/json
#               payload (offline fallback; network is tried first).
#   PY          python3 to use (default python3)
#   PIP         pip to use (default pip3)
set -uo pipefail

REQ="$(realpath "${1:?usage: verify_msgpack_lock.sh <requirements.txt> [results-dir]}")"
RESULTS="${2:-$(dirname "$0")/../results}"
mkdir -p "$RESULTS"
PY="${PY:-python3}"; PIP="${PIP:-pip3}"
# The mooncake e2e image: FROM python:3.12.13-slim, linux/amd64, --only-binary=:all:.
TARGET_PY=3.12
TARGET_PLATFORM=manylinux2014_x86_64

emit() { printf '%s\t%s\n' "$1" "$2"; }
note() { printf '  [%s] %s\n' "$1" "$2" >&2; }

# ---- extract the msgpack entry (version + hash set) ----
ENTRY_TXT="$($PY - "$REQ" <<'EOF'
import sys, re
lines = open(sys.argv[1]).read().splitlines()
start = next((i for i,l in enumerate(lines) if l.startswith('msgpack==')), None)
if start is None:
    sys.stderr.write('no msgpack entry\n'); sys.exit(1)
end = start + 1
while end < len(lines) and (lines[end].strip().startswith('--hash') or lines[end].strip().startswith('# via')):
    end += 1
print('\n'.join(lines[start:end]))
EOF
)" || { emit C1 fail; emit C2 fail; note C1 "no msgpack entry found in $REQ"; exit 1; }

"$PY" -c "import sys; sys.exit(0)" <<<"" >/dev/null  # noop guard for older hosts
VER="$(printf '%s\n' "$ENTRY_TXT" | head -1 | sed -E 's/^msgpack==([0-9A-Za-z.+-]+).*/\1/')"
printf '%s\n' "$ENTRY_TXT" > "$RESULTS/msgpack-entry.txt"

# ================= C1: hash-set integrity vs PyPI =================
C1=pass
PYPI_SRC=""
if [ -n "${PYPI_JSON:-}" ]; then
  PYPI_SRC="$PYPI_JSON"
elif curl -sf --max-time 60 "https://pypi.org/pypi/msgpack/${VER}/json" -o "$RESULTS/pypi-msgpack-${VER}.json" 2>/dev/null; then
  PYPI_SRC="$RESULTS/pypi-msgpack-${VER}.json"
  note C1 "fetched live PyPI metadata for msgpack ${VER}"
else
  PYPI_SRC="$(dirname "$0")/../results/pypi-msgpack-${VER}.json"
  note C1 "network fetch failed; using cached snapshot $PYPI_SRC"
fi

C1_OUT="$($PY - "$REQ" "$PYPI_SRC" "$VER" <<'EOF'
import sys, json, re
req_path, pypi_path, ver = sys.argv[1], sys.argv[2], sys.argv[3]
lines = open(req_path).read().splitlines()
start = next(i for i,l in enumerate(lines) if l.startswith('msgpack=='))
end = start + 1
while end < len(lines) and (lines[end].strip().startswith('--hash') or lines[end].strip().startswith('# via')):
    end += 1
pr_hashes = [re.search(r'sha256:([0-9a-f]{64})', l).group(1)
             for l in lines[start:end] if '--hash' in l]
pr_set = set(pr_hashes)
pypi = json.load(open(pypi_path))
files = {f['filename']: f['digests']['sha256'] for f in pypi['urls']}
pypi_set = set(files.values())
ok = True
if pypi['info']['version'] != ver:
    print('VERSION-MISMATCH pypi=%s req=%s' % (pypi['info']['version'], ver)); ok = False
extra = pr_set - pypi_set
missing = pypi_set - pr_set
if extra:    print('EXTRA %d hash(es) not on PyPI: %s' % (len(extra), sorted(extra)[:3])); ok = False
if missing:  print('MISSING %d PyPI digest(s): %s' % (len(missing), sorted(missing)[:3])); ok = False
if len(pr_hashes) != len(pr_set):
    print('DUPLICATE hash lines: %d lines / %d unique' % (len(pr_hashes), len(pr_set))); ok = False
# the wheel(s) the image platform actually resolves must be covered by the lock
need = [fn for fn in files if fn.startswith('msgpack-%s-cp312' % ver) and 'x86_64' in fn and 'linux' in fn and fn.endswith('.whl')]
if not need:
    print('NO cp312 linux x86_64 wheel on PyPI for %s' % ver); ok = False
else:
    for fn in need:
        if files[fn] not in pr_set:
            print('NEEDED-WHEEL-NOT-LOCKED %s' % fn); ok = False
print('ENTRY version=%s hashes=%d pypi-files=%d needed-wheels=%s' % (ver, len(pr_set), len(pypi_set), ','.join(need)))
print('C1_RESULT %s' % ('pass' if ok else 'fail'))
EOF
)" 2>>"$RESULTS/c1-python.err" || C1=fail
printf '%s\n' "$C1_OUT" > "$RESULTS/c1-detail.txt"
if printf '%s\n' "$C1_OUT" | grep -q 'C1_RESULT pass'; then C1=pass; else C1=fail; fi
note C1 "$(printf '%s' "$C1_OUT" | tr '\n' '; ' | tail -c 300)"

# ================= C2: pip --require-hashes resolves for the image platform =================
# Retries: the default index (mirrors on this host) is occasionally flaky at the
# transport layer; fall back to pypi.org so a mirror hiccup can't fake a FAIL.
# Final fallback (clearly labeled in the log): if the host pip itself dies with
# the known pip<24 vendored-urllib3 transport bug ("'>=' not supported" TypeError),
# fetch the exact needed artifact with curl and verify its sha256 against the lock
# — same artifact, same digest check, one step short of pip's own verifier.
C2=pass
C2_MODE=""
DL_DIR="$(mktemp -d)"
: > "$RESULTS/c2-pip-download.log"
pip_try() {  # $1 = pip binary, $2 = extra index args
  for attempt in 1 2 3; do
    if "$1" download --no-deps --only-binary=:all: --require-hashes \
          --python-version "$TARGET_PY" --platform "$TARGET_PLATFORM" --implementation cp \
          $2 -d "$DL_DIR" -r "$RESULTS/msgpack-entry.txt" \
          >> "$RESULTS/c2-pip-download.log" 2>&1; then
      return 0
    fi
    sleep $((attempt * 2))
  done
  return 1
}
pip_transport_bug() {  # pip<24 vendored-urllib3 "'>=' not supported" TypeError
  grep -q "not supported between instances of 'int' and 'NoneType'" "$RESULTS/c2-pip-download.log"
}
provision_modern_pip() {  # $1 = target dir; echoes the venv pip path, empty on failure
  # curl the newest pip wheel straight from files.pythonhosted.org (no simple-index
  # fetch, so the transport bug cannot bite); verify its digest from the JSON.
  local dir="$1" url sha fn
  "$PY" - <<'EOF' > "$dir/pipinfo.txt"
import json, urllib.request
try:
    d = json.load(urllib.request.urlopen('https://pypi.org/pypi/pip/json', timeout=60))
    for f in d['urls']:
        if f['filename'] == 'pip-%s-py3-none-any.whl' % d['info']['version']:
            print(f['url']); print(f['digests']['sha256']); print(f['filename'])
except Exception as e:
    pass
EOF
  url="$(sed -n 1p "$dir/pipinfo.txt")"; sha="$(sed -n 2p "$dir/pipinfo.txt")"; fn="$(sed -n 3p "$dir/pipinfo.txt")"
  [ -n "$url" ] && [ -n "$sha" ] || return 1
  curl -sfL --max-time 120 "$url" -o "$dir/$fn" || return 1
  [ "$(sha256sum "$dir/$fn" | cut -d' ' -f1)" = "$sha" ] || return 1
  "$PY" -m venv "$dir/venv" >/dev/null 2>&1 || return 1
  "$dir/venv/bin/pip" install --no-index "$dir/$fn" >/dev/null 2>&1 || return 1
  echo "$dir/venv/bin/pip"
}
PIP_BUG_DIR="$(mktemp -d)"
check_fetched_wheel() {  # digest re-check of what pip actually fetched
  WHL="$(ls "$DL_DIR"/*.whl 2>/dev/null | head -1)"
  if [ -n "$WHL" ]; then
    GOT="$(sha256sum "$WHL" | cut -d' ' -f1)"
    if grep -q "sha256:$GOT" "$RESULTS/msgpack-entry.txt"; then
      note C2 "downloaded artifact $(basename "$WHL") sha256 matches a locked hash"
    else
      C2=fail; note C2 "downloaded artifact digest NOT in lock: $GOT"
    fi
  else
    C2=fail; note C2 "no wheel downloaded"
  fi
}
if pip_try "$PIP" "" || pip_try "$PIP" "-i https://pypi.org/simple"; then
  note C2 "pip accepted the lock and downloaded the ${TARGET_PY}/${TARGET_PLATFORM} wheel"
  check_fetched_wheel
elif pip_transport_bug; then
  # known host-side pip<24 vendored-urllib3 transport bug. Provision a modern pip
  # (wheel curl'd straight from files.pythonhosted.org — no simple-index fetch)
  # so pip's own --require-hashes verifier still decides the claim.
  note C2 "host pip hit the vendored-urllib3 transport bug; provisioning a modern pip"
  MODPIP="$(provision_modern_pip "$PIP_BUG_DIR")"
  if [ -n "$MODPIP" ] && pip_try "$MODPIP" "-i https://pypi.org/simple"; then
    note C2 "modern pip accepted the lock and downloaded the ${TARGET_PY}/${TARGET_PLATFORM} wheel"
    check_fetched_wheel
    C2_MODE=" (via provisioned modern pip: host pip had the transport bug)"
  else
    # last resort: direct artifact download + digest comparison (labeled as such)
    note C2 "falling back to curl artifact check (pip's own verifier not exercised this run)"
    FB=pass
    while IFS=$'\t' read -r fn url digest; do
      [ -n "$fn" ] || continue
      if curl -sfL --max-time 120 "$url" -o "$DL_DIR/$fn"; then
        GOT="$(sha256sum "$DL_DIR/$fn" | cut -d' ' -f1)"
        if [ "$GOT" = "$digest" ] && grep -q "sha256:$GOT" "$RESULTS/msgpack-entry.txt"; then
          note C2 "fallback: $fn sha256 == PyPI digest == a locked hash"
        else
          FB=fail; note C2 "fallback: $fn digest mismatch (got $GOT, want $digest)"
        fi
      else
        FB=fail; note C2 "fallback: curl failed for $url"
      fi
    done < <("$PY" - "$PYPI_SRC" "$VER" <<'EOF'
import sys, json
pypi, ver = json.load(open(sys.argv[1])), sys.argv[2]
for f in pypi['urls']:
    if f['filename'].startswith('msgpack-%s-cp312' % ver) and 'x86_64' in f['filename'] \
       and 'manylinux' in f['filename'] and f['filename'].endswith('.whl'):
        print('%s\t%s\t%s' % (f['filename'], f['url'], f['digests']['sha256']))
EOF
)
    C2="$FB"; C2_MODE=" (curl fallback after pip failure; see c2-pip-download.log for pip's own error)"
  fi
else
  C2=fail; note C2 "pip download failed — see c2-pip-download.log"
fi
rm -rf "$PIP_BUG_DIR"
cp -f "$RESULTS/c2-pip-download.log" "$DL_DIR"/*.whl "$RESULTS/" 2>/dev/null || true
rm -rf "$DL_DIR"

# ================= verdict lines (consumed by re-verify.sh) =================
printf 'C1\t%s\n' "$C1" > "$RESULTS/verdicts.tsv"
printf 'C2\t%s%s\n' "$C2" "$C2_MODE" >> "$RESULTS/verdicts.tsv"
emit C1 "$C1"
emit C2 "$C2$C2_MODE"
[ "$C1" = pass ] && [ "$C2" = pass ] && exit 0 || exit 1
