# Verification: PR #6201 — bump urllib3 2.7.0 → 2.8.0 in the mooncake e2e image lock file

PR: https://github.com/fluid-cloudnative/fluid/pull/6201 (Dependabot)
Reviewed head: `fac7daf4e1cd7a6d1d896ac09619a2c84273352e` (base `765e27d6`)
Change: one entry in `test/gha-e2e/mooncake/image/requirements.txt`
(`urllib3==2.7.0` +1 hash → `urllib3==2.8.0` +2 hashes). Test-only image;
production code untouched.

## Premise (P0) — CONFIRMED, with an exposure caveat

Claim (PR body): urllib3 2.8.0 fixes three security issues present in the
pinned 2.7.0. Verified two ways:

1. GitHub advisory API: GHSA-8988-9cw3-xx77 (high), GHSA-vxq7-64xx-v4gw (high),
   GHSA-gh4c-6fx4-qh6g (medium) all list affected ranges `< 2.8.0`, patched in
   2.8.0 — see `results/ghsa-affected-ranges.txt`.
2. Reproduction of GHSA-vxq7-64xx-v4gw on the base pin: a local server sends a
   4 MiB unterminated chunk-size line. urllib3 **2.7.0** buffers the whole line
   (peak traced 8.6 MB) before failing; urllib3 **2.8.0** raises
   `ProtocolError: Response chunk size line exceeded maximum allowed length`
   after 64 KiB (peak 284 KB). See `results/p0-urllib3-2.7.0.txt` /
   `results/p0-urllib3-2.8.0.txt`.

Exposure caveat: in this image urllib3 arrives via `requests`, and
`mooncake-transfer-engine-non-cuda==0.3.12.post1` never imports `requests` or
`urllib3` in its python code (checked the wheel), and the e2e runs cluster-
internal plain HTTP with no proxies. So the CVEs are effectively unreachable
here; the bump is still correct scanner hygiene and keeps the lock file on a
maintained release.

## Observed vs expected

| claim | layer | polarity | expected (fixed) | observed base | observed PR head | verdict |
|---|---|---|---|---|---|---|
| P0: 2.7.0 vulnerable, 2.8.0 fixed | unit | contract | 2.8.0 bounds chunk-size line at 64KiB | 2.7.0 buffered 8.6MB (exit 1) | 2.8.0 ProtocolError, 284KB peak (exit 0) | confirmed |
| F1-check: PR hashes match PyPI digests; requires_python ⊇ py3.12; reverse-dep constraints admit 2.8.0 | unit | contract | pass | exit 1 (pin is 2.7.0 — bites check) | pass (exit 0) | verified |
| F1-check: merged lock file resolves for cp312/manylinux x86_64 with `--require-hashes` | integration | contract | 17 artifacts downloaded+verified | n/a | 17/17 incl. urllib3-2.8.0 wheel | verified |
| F2-check: requests 2.34.2 + urllib3 2.8.0 runtime round-trip | integration | contract | GET 200 + streamed read | n/a | 200, 2244 bytes streamed | verified |

No defects found in the PR. Both Dependabot-added sha256 values equal PyPI's
published digests (`0cf3cae5…` wheel, `63bf2ead…` sdist); artifacts are not
yanked; `requests 2.34.2` allows `urllib3<3,>=1.26`; mooncake declares urllib3
only transitively with no upper bound. The kind-e2e GHA workflow builds this
image (`WITH_E2E_TEST_IMAGES=true`, `docker build test/gha-e2e/mooncake/image`,
which runs `pip install --require-hashes`) and runs the mooncake e2e on every
PR — the change is exercised by CI.

## How to run

```
V=docs/verification/mooncake-urllib3-bump
# L1 static lock-file check (contract; fails on the base ref's file)
python3 $V/scripts/l1_lockfile_static_check.py test/gha-e2e/mooncake/image/requirements.txt
# L2a full lock-file resolution for the image platform (needs pip>=24 in \$VENV or auto-created)
VENV=/tmp/verify-venv bash $V/scripts/l2_full_lockfile_resolve.sh test/gha-e2e/mooncake/image/requirements.txt
# L2b runtime smoke in a venv with the pinned pair
python3 -m venv /tmp/vsmoke && /tmp/vsmoke/bin/pip install requests==2.34.2 urllib3==2.8.0
/tmp/vsmoke/bin/python $V/scripts/l2_runtime_smoke.py
# P0 premise repro, one venv per urllib3 version
python3 -m venv /tmp/v27 && /tmp/v27/bin/pip install urllib3==2.7.0 && /tmp/v27/bin/python $V/scripts/p0_chunked_line_repro.py 2.7.0   # expect exit 1
python3 -m venv /tmp/v28 && /tmp/v28/bin/pip install urllib3==2.8.0 && /tmp/v28/bin/python $V/scripts/p0_chunked_line_repro.py 2.8.0   # expect exit 0
```

Host used: python 3.11 x86_64 (venv scripts create their own envs); L2a targets
cp312/manylinux x86_64 via pip's `--platform/--python-version/--abi` flags, so no
docker or python 3.12 host is needed. Network access to pypi.org and
api.github.com is required.

## Continuing after the fix / from another machine

`bash docs/verification/mooncake-urllib3-bump/scripts/re-verify.sh [ref]`
fetches the current PR head (from `verify-manifest.json`'s `pr` URL), prints the
incremental delta since `.last-reviewed`, and re-runs L1/L2/P0 reporting
Fixed/Still-broken per finding. All tests are contract polarity: green = fixed.
If Dependabot bumps further (e.g. 2.9.x), update `EXPECTED_URLLIB3` in
`l1_lockfile_static_check.py` and the pin in the L2b/P0 commands (Harness-update).

Kickoff prompt for a fresh agent:
> Check out branch verify/mooncake-urllib3-bump-codex from the reviewer's fork,
> read docs/verification/mooncake-urllib3-bump/README.md, then run
> docs/verification/mooncake-urllib3-bump/scripts/re-verify.sh and report the
> per-finding verdicts.
