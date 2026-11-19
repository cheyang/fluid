# Verification — PR #6269 "fix(security): clear the remaining code scanning and Dependabot alerts"

Reviewer: **Codex (Reviewer B)**. Reviewed head `7ec63f64207653baeec60fb60b1a05ec02aaf4f8`, base
`dcb76167b0143a239addc6bed83ed50b45267631`. Production code untouched — this branch adds only tests,
scripts and this evidence.

## Premise (P0) — CONFIRMED, runs against base

The PR claims to clear the still-open security alerts:

| claim | verified | evidence |
|---|---|---|
| Dependabot 82–101: 17 aiohttp advisories on `test/gha-e2e/mooncake/image/requirements.txt` | yes — exactly 17 open pip/aiohttp alerts, all on that manifest | `results/premise-dependabot-alerts.txt` |
| Dependabot 102: CVE-2024-5206 (scikit-learn 1.4.0) on `samples/vineyard/requirements.txt` | yes — alert 102 open, GHSA-jw8x-6495-233v | same file |
| Code scanning 1481/1482 (pythonsecurity:S8707 path injection) on `hack/sdk/post-gen.py` | yes — open on base commit `dcb76167`, at the two `open()` calls (lines 25/29) | `results/premise-code-scanning-alerts.txt` |
| Code scanning 1490–1497, 1502, 1504, 1513–1515, 1551 (14 alerts) on `sdk/python` submodule | yes — all open on base, each path/line maps to a file the submodule bump changes | same file |
| aiohttp 3.14.3 is the vulnerable version; 3.14.5 fixes it | yes — OSV: 24 records (17 GHSA) for 3.14.3, **0** for 3.14.5 | `results/premise-osv.txt` |
| scikit-learn 1.4.0 vulnerable; 1.7.2 clean | yes — OSV: 2 records for 1.4.0, 0 for 1.7.2 | same file |
| #6212 (aiohttp-only bump) can never build | yes — reproduced `ResolutionImpossible` (aiohttp 3.14.4 requires yarl>=1.25.1, lock pins 1.24.5) | `results/mooncake-resolve.txt` |

## Findings / claims verified

| id | claim | layer | result |
|---|---|---|---|
| V1 | `post-gen.py` accepts only the generated SDK dir (all spellings), rejects everything else, rewrite output byte-identical to base | unit (`hack/sdk/tests/test_post_gen.py`) | PASS on head; reject-matrix FAILS on base script = harness bites |
| V2 | head mooncake lock resolves on the exact base image; #6212 variant fails; base lock resolves (control) | integration (`scripts/pip_resolve_in_container.sh`) | PASS |
| V3 | every hash in both changed locks + submodule `build-requirements.txt` is a real PyPI artifact for the pinned version | integration (`scripts/check_locks_against_pypi.py`, `scripts/check_submodule_build_reqs.py`) | PASS (28+17+7 packages, 0 unknown hashes) |
| V4 | vineyard Dockerfile's exact pip command succeeds on pinned `python:3.10` and the sample's sklearn flow runs (`pip check` clean) | docker amd64 **and** arm64 (qemu) | PASS both arches — `results/docker-vineyard-{amd64,arm64}.log` |
| V5 | mooncake e2e image builds from the head Dockerfile; runs with aiohttp 3.14.5 / yarl 1.25.1 | docker | PASS — `results/docker-mooncake.log` |
| V6 | submodule `b06de4e8` == fluid-client-python master HEAD == merge of PR #12; only the claimed 8 files differ; tree == PR-head tree; pinned action SHAs match their tags | integration (`scripts/verify_submodule.sh`) | PASS — `results/submodule-verify.txt` |

Not verified here (author committed to check post-merge): SonarCloud closing 1481/1482 and the
14 `sdk/python` alerts on the next *master* analysis — its PR analysis only reports new issues and
CI never checks out submodules. All static evidence (allow-list constant return, seeded
`train_test_split`, `weights_only=True`, ADD→COPY, non-root USER, `/data` confinement, SHA-pinned
actions, hash-locked build deps) matches each alert's rule; loopback `127.0.0.1` is S1313-compliant
per SonarSource's exception for loopback addresses.

## How to run

```bash
# unit (post-gen.py allow-list; POST_GEN_SCRIPT=hack/sdk/post-gen.base.py runs the polarity check)
python3 -m unittest discover -s hack/sdk/tests -v

# integration (needs network; docker only for pip_resolve_in_container.sh)
python3 docs/verification/pr6269-security-alerts/scripts/check_locks_against_pypi.py
python3 docs/verification/pr6269-security-alerts/scripts/check_submodule_build_reqs.py
bash    docs/verification/pr6269-security-alerts/scripts/pip_resolve_in_container.sh
bash    docs/verification/pr6269-security-alerts/scripts/verify_submodule.sh

# docker layer (needs docker + qemu binfmt for arm64)
bash    docs/verification/pr6269-security-alerts/scripts/docker_lock_checks.sh all
```

## Continuing after a revision

`bash docs/verification/pr6269-security-alerts/scripts/re-verify.sh` fetches the current PR head,
grafts this harness onto it, and re-runs unit + integration layers (docker layer when docker is
available). All checks are contract-polarity: they must keep passing. The review delta for the next
round is `.last-reviewed`..new-head; advance the marker afterwards.
