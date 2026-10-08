# multidict-sec-bump — verification harness

Reproducible evidence for the review of
[PR fluid-cloudnative/fluid#6202](https://github.com/fluid-cloudnative/fluid/pull/6202)
(Dependabot security bump: `multidict` 6.7.1 → 6.9.1 in
`test/gha-e2e/mooncake/image/requirements.txt`).

Branch: `verify/multidict-sec-bump-claude`, based on the PR head
(`c68356d7e4d52421095a14d2fc9e5fd5fb764e64`). **Production code is untouched** —
the only diff vs the PR is this `docs/verification/multidict-sec-bump/` tree.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| L0 (static) | every pinned hash is a genuine PyPI digest; hash-scope vs documented platform | `python3 scripts/lock_integrity.py test/gha-e2e/mooncake/image/requirements.txt` |
| L1 (resolution) | the Dockerfile's `pip install --require-hashes` step for the image platform (cp312 / manylinux x86_64), simulated cross-platform | see `scripts/run_all.sh` (uses `pip download --python-version 312 ...`) |
| L2 (runtime) | GHSA leak probe (base vs PR pin), compatibility smoke with pinned aiohttp/yarl | `bash scripts/run_all.sh` |
| L3 (live) | **skipped** — probe found a reachable cluster, but the debate round mandates no live layer; the change is a test-image dependency pin with no in-cluster behavior of its own. CI's `kind-e2e-test` (green, 5 jobs) already builds this exact image and runs the mooncake e2e case against it — see `results/05-ci-evidence.txt`. |

> Prereqs for L1/L2: python3 ≥ 3.10 with venv, network to PyPI (or a configured
> mirror), ~90 MB of downloads. Runs on any host Python; the target platform is
> simulated via pip's cross-environment flags.

> Test polarity: **contract** tests assert intended behavior (fail on the
> defect, pass when fixed). `run_all.sh` exits 0 with F1 red — F1 red *is* the
> finding; see the summary block.

## Problem premise (P0)

Run against the **base** branch (merge-base `765e27d6`), without the patch.

| | |
|---|---|
| Claimed symptom | Dependabot security alert on `multidict` pinned at 6.7.1 in the mooncake e2e image lock; GHSA-54p9-h82j-f925 / CVE-2026-104874: "A reference leak in the items-view union and subtraction operators ... lets a remote client drive unbounded, unreclaimable memory growth" |
| Linked issue | none (Dependabot security-update PR; advisory range `>= 6.7.0, <= 6.9.0`, first patched **6.9.1** — 6.7.1 is inside the range) |
| Reported component | `multidict` C extension (`multidict_itemsview_or2_impl` / `multidict_itemsview_sub1_impl`) |
| Patched component | `test/gha-e2e/mooncake/image/requirements.txt` — the one place in the repo that pins multidict (transitive dep of `mooncake-transfer-engine-non-cuda` via aiohttp) |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | leak probe on 6.7.1: `or2`/`sub1` leak exactly 100000 refs (= operand size), controls leak 0; the PR pin 6.9.1 leaks 0 — `results/30-p0-base-6.7.1.txt`, `results/40-p0-pr-version.txt` |

Reachability honesty: the advisory's exploit path is an aiohttp *server*
evaluating items-view set algebra over remote-controlled operands. The mooncake
e2e image is a short-lived, kind-cluster-internal test workload; actually
triggering the leak there is unlikely. The bump is still correct alert hygiene
and Dependabot picked the maximal safe version (7.0.0 exists but the pinned
`aiohttp==3.14.3` requires `multidict<7.0`).

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | base pin 6.7.1 is vulnerable to GHSA-54p9-h82j-f925 | L2 | **Confirmed** (leak = 100000 on or2/sub1, controls 0) | `results/30-p0-base-6.7.1.txt` |
| V1 | the PR lock resolves for cp312/manylinux x86_64 and every pinned hash is a genuine PyPI digest | L0+L1 | **Confirmed** (17/17 entries authentic; full lock downloads with hashes verified) | `results/10-lock-integrity.txt`, `results/20-pip-download.txt` |
| V2 | multidict 6.9.1 is compatible with pinned aiohttp 3.14.3 / yarl 1.24.5 | L2 | **Confirmed** (smoke + `pip check` clean; resolver rejects multidict 7.0.0) | `results/41-compat-smoke.txt` |
| F1 | the multidict entry pins all 171 release artifacts instead of the single documented platform hash, weakening the lock's fail-loudly property | L0 | **Confirmed** (contract red: 171 hashes, 168 off-platform; zeroed cp312 hash → pip silently falls back to `py3-none-any` wheel instead of failing) | `results/50-hash-scope.txt` |

## Per-finding detail

### P0 — premise (confirmed)

Adapted the advisory's own PoC into `scripts/leak_probe.py` (contract polarity:
leak == 0). Base 6.7.1: red (leaks). PR 6.9.1: green. The differential is also
the harness-bites proof for this test.

### V1 — lock integrity + image-build resolution (confirmed)

- `lock_integrity.py`: all 17 pinned entries' hashes map to genuine PyPI
  artifacts of the pinned versions — no fabricated digests (supply-chain check
  on a hash-locked file). Note the PR branch point predates master's urllib3
  2.8.0 bump (`fac7daf4`); both bumps touch disjoint blocks and the PR is
  `MERGEABLE`.
- Cross-platform `pip download` for the image's exact target
  (`--python-version 312 --abi cp312 --platform manylinux_2_28_x86_64
  --platform manylinux2014_x86_64 --only-binary=:all: --require-hashes`)
  resolves and downloads all 17 wheels with hashes verified — the Dockerfile's
  install step cannot fail on a hash/version mismatch.
- Belt and braces: CI's `kind-e2e-test` (5 k8s versions, all SUCCESS) already
  built the image from this lock and ran the mooncake e2e case.

### V2 — compatibility (confirmed)

`scripts/compat_smoke.py` + `pip check` in a venv with the PR pin set:
case-insensitive lookup, repeated-header `getall`, the two fixed set-op paths
returning correct *results*, yarl query parsing. Bite check: the resolver
rejects `multidict==7.0.0` next to `aiohttp==3.14.3` (`ResolutionImpossible`),
so the `<7.0` constraint the review relies on is enforced by pip, not assumed.

### F1 — hash-scope of the multidict entry (confirmed, minor)

The file's own header documents the convention: wheels are
"cp312 / manylinux x86_64", and the regeneration procedure produces exactly the
artifact pip reports for that platform. All 16 other entries follow it (1 hash
each; the merged urllib3 bump used 2 = wheel+sdist of a pure-python package).
The PR's Dependabot entry instead pins **all 171 artifacts of 6.9.1**
(macos/windows/android/musl/ppc64le/... 168 of them off-platform).

Consequence, demonstrated in `results/50-hash-scope.txt`: with the cp312 wheel's
hash line invalidated, pip does **not** fail the build — it silently installs
`multidict-6.9.1-py3-none-any.whl` (pure-python, no C extension), because that
hash is also in the list. The header's stated guarantee ("--require-hashes
makes pip reject any resolution that strays from this file") is weakened from
*reject* to *substitute*: a transcription error or index drift on the platform
wheel degrades the image without any red build. With the single-hash
convention the same corruption is a hard failure (proven in the mini control:
one wrong hash → `THESE PACKAGES DO NOT MATCH THE HASHES`).

## Proposed fix (NOT applied here — production code untouched)

Trim the multidict entry to the one artifact the image build can select:

```
multidict==6.9.1 \
    --hash=sha256:976fd7689d69ec78d67d31d38d396d8adb562f7e8368279f76aed4aa451fa06d
    # via mooncake-transfer-engine-non-cuda
```

(`976fd768…` = `multidict-6.9.1-cp312-cp312-manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64.whl`,
exactly what the file's documented `pip install --report` procedure yields.)
Alternatively keep the Dependabot list and accept the churn — the choice is the
maintainers'; severity is minor either way. Trimming makes F1's contract test
green and restores the fail-loudly property.

## Live run notes (L3, skipped)

Cluster probe succeeded (2 Ready nodes, v1.36.2) but this debate round runs
unit+integration only. Nothing in this PR has in-cluster behavior beyond the
image contents; the kind-e2e CI matrix is the live equivalent and is green.

## Continuing after the fix (possibly on another machine)

All durable state is on this branch. From any clone of fluid:

```bash
git fetch https://github.com/cheyang/fluid.git verify/multidict-sec-bump-claude
git checkout verify/multidict-sec-bump-claude
bash docs/verification/multidict-sec-bump/scripts/re-verify.sh
```

With no argument it fetches the current head of PR #6202 from the manifest's
`pr` URL, grafts the harness onto it in a temp worktree, and re-runs
L0–L2. Verdicts: **P0 Fixed** when the pinned multidict no longer leaks;
**F1 Fixed** when the entry has exactly the single platform hash (the contract
in `hash_scope_check.sh` flips green); **V1/V2 OK** while the lock resolves and
the pin set stays compatible. `re-verify.sh` prints the
`last-reviewed..head` delta and how to advance `.last-reviewed`.

Kickoff prompt for a fresh agent:

> You are resuming a verification round for
> https://github.com/fluid-cloudnative/fluid/pull/6202. Checkout branch
> `verify/multidict-sec-bump-claude` (from the cheyang fork), read
> `docs/verification/multidict-sec-bump/README.md`, run
> `scripts/re-verify.sh`, apply polarity from the README, report per finding
> Fixed / Still-broken / Partial, then advance `.last-reviewed` and push.
