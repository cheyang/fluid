# Verification — PR #6215: bump certifi 2026.7.22 → 2026.10.19 (mooncake e2e image)

Reviewer: Codex (Reviewer B). Round: initial. Base: `86e3adf3`, head reviewed: `e0f01cc5`.
Harness touches **no** production code; it only reads
`test/gha-e2e/mooncake/image/requirements.txt` at a given ref and checks it against PyPI.

## Premise (P0) — CONFIRMED, runs against BASE

GHSA-6q48-6c7w-9gw2 (critical, published 2026-10-20): certifi `>= 2019.03.09, < 2026.10.19`
bundles `emSign ECC Root CA - G3`; eight eMudhra intermediate CAs chaining to it have
compromised private keys (on-path MITM of TLS clients trusting the bundle, e.g. `requests`).
The base pin `certifi==2026.7.22` is in the vulnerable range; the PR pins the first patched
version `2026.10.19`.

Evidence (`results/p0-bundle-delta.txt`): the 2026.7.22 wheel bundle has 121 certs including
`CN = emSign ECC Root CA - G3`; the 2026.10.19 bundle has 120 and the **only** delta is the
removal of that root (three unaffected emSign roots remain, matching certifi #437/#440).

## Claims and results (observed vs expected)

| id | claim | polarity | layer | expected on head | observed | verdict |
|----|-------|----------|-------|------------------|----------|---------|
| P0 | base pin trusts emSign ECC Root CA - G3 (premise) | premise | unit | present on base / absent on head | present on base (exit 1), absent on head (exit 0) | **confirmed** |
| C1 | pin ≥ 2026.10.19, hashes == official PyPI artifacts, wheel hash present, satisfies all pinned dependents' certifi constraints | contract | unit | pass | pass (`results/c1-pin-check.txt`) | verified |
| C2 | pip accepts the shipped entry under `--only-binary=:all: --require-hashes`; corrupted-hash / sdist-only / version-mismatch entries rejected | contract | unit | pass + 3 controls bite | pass (`results/c2-pip-hash-gate.txt`) | verified |
| CI | image builds and mooncake e2e passes on the head | — | integration (GHA) | kind-e2e-test green | 5/5 k8s versions SUCCESS at e0f01cc (`results/ci-status.txt`) | verified |

L3 (live) intentionally skipped: the artifact is an e2e-only image; the PR's own
kind-e2e-test workflow is the integration layer (it builds this exact image with
`--require-hashes` and runs the mooncake client-less topology e2e).

## How to run

From a checkout of this branch:

```bash
# everything, against the current PR head (auto-fetched via manifest.pr):
bash docs/verification/certifi-emsign-mooncake-e2e/scripts/re-verify.sh
# or against an explicit ref:
bash docs/verification/certifi-emsign-mooncake-e2e/scripts/re-verify.sh <sha>
```

Individual checks (take a requirements.txt path, default the in-tree file):

```bash
python3 docs/verification/certifi-emsign-mooncake-e2e/scripts/check_certifi_pin.py   [reqs]
bash    docs/verification/certifi-emsign-mooncake-e2e/scripts/pip_hash_gate.sh       [reqs]
python3 docs/verification/certifi-emsign-mooncake-e2e/scripts/check_bundle_delta.py  [reqs]  # exit 1 if G3 present
```

Prereqs: python3, pip (venv created on the fly), openssl, network to pypi.org.

## Polarity note for future rounds

All findings are **contract** checks: they PASS while the pin is correct and FAIL if a
future revision reintroduces a vulnerable pin or a bad hash. `check_bundle_delta.py`
inverts the premise: exit 1 means the pinned bundle still trusts the compromised root
(expected on base, failure on any merge candidate).

## Notes for the maintainer (non-blocking)

- certifi 2026.10.23 (released 2 days after this PR) additionally drops the two remaining
  eMudhra Inc roots (C1, C3); no GHSA is attached to that release and 2026.10.19 fully
  closes GHSA-6q48-6c7w-9gw2. Dependabot will file the follow-up.
- The new entry lists both sdist and wheel hashes; the file's header documents the
  regen procedure (pip dry-run report) which records only the resolved wheel hash.
  Harmless under `--only-binary=:all:` (control 2 in C2 shows the wheel hash is the
  load-bearing one).
