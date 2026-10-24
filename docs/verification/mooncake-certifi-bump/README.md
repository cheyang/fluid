# mooncake-certifi-bump — verification harness

Reproducible evidence for the review of [PR #6215](https://github.com/fluid-cloudnative/fluid/pull/6215)
(Dependabot: bump certifi 2026.7.22 → 2026.10.19 in `test/gha-e2e/mooncake/image`).

Layers run against the **code under review** (`e0f01cc527f4a0fdbefb51babb25575b5931146e`, PR head):

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (deterministic) | lockfile integrity: every pin resolves on PyPI, every pinned sha256 matches a real artifact of that exact version, an installable py3.12/manylinux-x86_64 wheel exists | `python3 docs/verification/mooncake-certifi-bump/scripts/check_lockfile_integrity.py test/gha-e2e/mooncake/image/requirements.txt` |
| 2. Integration | the Dockerfile RUN-step contract (`pip install --only-binary=:all: --require-hashes`) reproduced with pip's cross-env flags for python:3.12.13-slim/linux-amd64: full-lockfile download + hash check, real install of the changed certifi stanza, and a harness-bites self-test (tampered hash must be rejected) | `bash docs/verification/mooncake-certifi-bump/scripts/check_pip_resolve.sh` |
| 3. Live | **skipped** per review policy (no cluster). The real-world equivalent — kind-e2e building this image and running the mooncake suite — is green on this head (see `results/ci-status.txt`) | — |

> Test polarity: all claims below are **contract** tests (they assert the intended
> correct behavior). They PASS on this head; a future bump that breaks the
> lockfile (wrong hash, no installable wheel, version that does not exist) turns
> them red. The harness-bites self-test additionally proves the checks can fail.

## Problem premise (P0)

Answered before the findings, run against the **base** branch without the patch.

| | |
|---|---|
| Claimed change | "Bumps certifi from 2026.7.22 to 2026.10.19" (Dependabot, security maintenance) |
| Linked issue | none (no closing keyword); premise taken from the PR title/body |
| Reported component | `test/gha-e2e/mooncake/image/requirements.txt` — hash-locked deps of the in-repo mooncake e2e helper image, built by `kind-e2e` on every PR |
| Patched component | same file, certifi stanza only (3 insertions, 2 deletions, no collateral edits) |
| Component match | **Yes** |
| **Verdict** | **Confirmed** — the pin is outdated and the update is real and security-relevant |
| Evidence | base lockfile resolves and hash-verifies (`results/control-base-resolve.log`, `results/l1-control-base.txt`); upstream 2026.10.19 exists, not yanked, and its `cacert.pem` differs from 2026.7.22 by exactly **one removed root: `emSign ECC Root CA - G3`** (121 → 120 roots) — `results/l2-ca-bundle-diff.txt` |

Note on scope: a Dependabot bump owes a problem statement only in the weak sense
("the pinned release is behind upstream"); the strong check here is that the bump
is *well-formed* and *behaviorally understood* — both proven below.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| C1 | every lockfile entry (incl. new certifi==2026.10.19 with two hashes) pins a real PyPI version whose pinned sha256 values match real artifacts of that version, with an installable py3.12/manylinux-x86_64 wheel | 1 | **pass** (17/17) | `results/l1-lockfile-integrity.txt` |
| C2 | pip resolves the entire PR-head lockfile with `--require-hashes --only-binary=:all:` for the image's target interpreter/platform — the exact Dockerfile RUN contract | 2 | **pass** (17/17 wheels downloaded + hash-verified) | `results/l2-pip-resolve.txt`, `results/l2-full-resolve.log` |
| C3 | the changed certifi stanza installs under `--require-hashes` and the 2026.10.19 CA bundle loads with a sane cert count | 2 | **pass** (120 certs) | `results/l2-certifi-install.log` |
| C4 | harness-bites: the same install with a tampered certifi hash is rejected by pip (`--require-hashes` actually enforced) | 2 | **pass** (tamper rejected) | `results/l2-harness-bites.log` |

## Per-finding detail

### The two new hashes
- Pinned: `035c1e85…` and `42d35340…`.
- `035c1e85…` = sha256 of the `certifi-2026.10.19.tar.gz` **sdist**; `42d35340…` = sha256
  of `certifi-2026.10.19-py3-none-any.whl` (verified against
  `https://pypi.org/pypi/certifi/2026.10.19/json` and by re-hashing the downloaded wheel).
- The image installs with `--only-binary=:all:`, so pip only ever fetches the **wheel**;
  its hash is pinned (the second one). The sdist hash is redundant but harmless
  (pip treats multiple `--hash` as OR), and multi-hash entries already exist in this
  file (`urllib3==2.8.0` from a prior Dependabot bump, `msgpack` with all its wheels).

### Behavioral delta of the bump
- `scripts/ca_bundle_delta.py` → `results/l2-ca-bundle-diff.txt`:
  2026.7.22 → 2026.10.19 removes exactly one trust root (`emSign ECC Root CA - G3`),
  adds none. This matches upstream's remove-emsign (#437) + restore-unaffected-roots
  (#439/#440) sequence.
- Blast radius in this image: `custom-entrypoint.sh` and `reportSummary.sh` only speak
  `http://localhost` (no TLS); the only TLS-capable code is transitive
  (requests/aiohttp inside mooncake) against in-cluster plain-HTTP services in the e2e.
  No functional impact expected — and all 6 kind-e2e matrix entries are SUCCESS on this head.
- `requires_python` of 2026.10.19 is `>=3.7`; the image runs Python 3.12.13. Compatible.

### CI
- `results/ci-status.txt`: PR OPEN, MERGEABLE, not draft; all checks SUCCESS including
  `kind-e2e-test` on all 6 Kubernetes versions (which builds this image via
  `build-all-images.sh` with `WITH_E2E_TEST_IMAGES: "true"` and runs the mooncake e2e).

## Proposed changes to production code
None. No defect was found. The PR is a well-formed, low-risk, security-motivated
dependency bump.

## Continuing after the fix (possibly on another machine)

There is nothing to fix; this harness is now this lockfile's regression suite. If the
PR (or a future bump to the same file) changes:

```bash
git fetch https://github.com/fluid-cloudnative/fluid.git pull/6215/head   # or the future PR's head
git checkout verify/mooncake-certifi-bump-claude
bash docs/verification/mooncake-certifi-bump/scripts/re-verify.sh          # auto-resolves current PR head
# or: bash docs/verification/mooncake-certifi-bump/scripts/re-verify.sh <ref>
```

Prereqs: git, jq, python3 (venv), network to PyPI. A broken host pip (seen on this
reviewer box: pip 23.3.1 vendored-urllib3 TypeError) is worked around automatically by
bootstrapping a current pip via `get-pip.py`.

Reading the output: all claims are contract tests — `FIXED`/pass means green,
`STILL-BROKEN`/fail means the lockfile contract is broken. `L2-HARNESS-BITES`
failing means either the tamper self-test stopped tampering (stanza shape changed)
or pip stopped enforcing `--require-hashes` — treat it as a harness bug first.

### Harness-bites already proven this round
`results/l2-harness-bites.log` shows pip rejecting the zeroed-out wheel hash. Two
harness bugs were found and fixed while building it (both instructive):
1. the L1 parser kept pip-compile's line-continuation backslash in parsed hashes
   (multi-hash entries failed spuriously);
2. the tamper originally hand-typed the hash literal, typo'd it, and silently
   tampered nothing — the tamper is now derived from the stanza with an assertion
   that it applied.

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/mooncake-certifi-bump-claude (fork
cheyang/fluid). Background: PR fluid-cloudnative/fluid#6215 bumps certifi
2026.7.22→2026.10.19 in test/gha-e2e/mooncake/image/requirements.txt; a review
found no defects and left a 4-claim contract harness (lockfile integrity, pip
--require-hashes resolution for py3.12/manylinux, certifi install, harness-bites).
Read docs/verification/mooncake-certifi-bump/README.md ("Continuing after the
fix") and follow it: re-run scripts/re-verify.sh against the current PR head,
report the observed-vs-expected table, and advance .last-reviewed if the head
moved. Do not run cluster-wide destructive actions; there is no live layer.
```
