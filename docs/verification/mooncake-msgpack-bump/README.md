# mooncake-msgpack-bump — lock verification

Reproducible evidence for the review of
[PR #6226](https://github.com/fluid-cloudnative/fluid/pull/6226) (dependabot:
bump msgpack 1.2.2 → 1.2.3 in `test/gha-e2e/mooncake/image/requirements.txt`).

Harness branch: `verify/mooncake-msgpack-bump-claude` (pushed to the reviewer's
fork `cheyang/fluid`). **Production code untouched** — the diff is this directory
only.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit | hash-set integrity of the msgpack entry vs PyPI's published digests | `bash scripts/re-verify.sh` (or `scripts/verify_msgpack_lock.sh <requirements.txt>`) |
| 2. Integration | pip's real `--require-hashes` resolution + hash verification for the image platform (python 3.12, manylinux x86_64, wheels only — exactly the Dockerfile's constraint set) | same script, claim C2 |
| 3. Live | docker build + mooncake e2e | **not run here** (round policy); covered by the PR's own CI (see below) |

> Polarity: both claims are **contract** tests — they assert the intended correct
> lock and FAIL on a tampered one. No bug-canaries.

## Problem premise (P0) — run against the BASE branch

| | |
|---|---|
| Claimed symptom | msgpack 1.2.2 is vulnerable; PR body cites GHSA-j586-36cw-2gc2 |
| Linked issue | none (dependabot security-update PR, generated from the repo's security alert) |
| Reported component | msgpack-python C extension (transitive dep of `mooncake-transfer-engine-non-cuda`) |
| Patched component | `test/gha-e2e/mooncake/image/requirements.txt` — the only msgpack pin in the repo |
| Component match | Yes |
| **Verdict** | **Confirmed** |
| Evidence | OSV: GHSA-j586-36cw-2gc2 / CVE-2026-114613, **HIGH**, range `introduced: 0 → fixed: 1.2.3`; base pins `msgpack==1.2.2` (`results/osv-msgpack-1.2.2.json`, `results/base-msgpack-entry.txt`) |

Caveat on real-world exposure: this pin lives only in the e2e test image that
runs on a throwaway kind cluster; the leaked/freed paths require
attacker-influenced malformed msgpack data, which the e2e harness does not feed.
So the practical severity for this repo is low — the bump is hygiene, and a
correct, minimal, hash-verified one.

## Summary of results (head `0960c0d6fab76b1097535ea9352f3fbd6131b617`)

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| C1 | msgpack entry's 91 `--hash` values exactly equal PyPI's 91 published sha256 digests for 1.2.3; needed cp312 manylinux x86_64 wheel digest is among them | 1 | **Pass** | `results/c1-detail.txt` (EXTRA: none, MISSING: none) |
| C2 | pip's real `--require-hashes` resolution for python 3.12 / manylinux x86_64 / wheels-only accepts the entry; fetched wheel's sha256 matches a locked hash | 2 | **Pass** | `results/c2-pip-download.log` (provisioned pip 26.2.2: "Successfully downloaded msgpack"); fetched wheel digest `ede33b28…` == PyPI digest == a lock entry. Independently repeated with a hand-installed pip 26.2.2 venv: `results/c2-pip26-direct-success.log` |

> Host quirk encountered (not a lock problem): the review machine's system pip
> 23.3.1 intermittently dies fetching the simple index with a vendored-urllib3
> `TypeError: '>=' not supported` transport bug. The harness detects that
> signature, provisions a modern pip (wheel curl'd directly from
> files.pythonhosted.org, digest-verified), and re-runs; only if that also fails
> does it fall back to a labeled curl artifact check.

Harness-bites: two tampered copies of the lock both go red for the right reasons
(corrupting a non-platform wheel's digest → C1 fails; corrupting the
platform-needed wheel's digest → C1 **and** C2 fail, C2 via pip's
`THESE PACKAGES DO NOT MATCH THE HASHES`) —
`results/harness-bites/SUMMARY.md`.

Additional review checks (no defects found):

- The diff touches exactly one entry of one file; no other msgpack pin exists in
  the repo (`grep -rn msgpack` → only this file and a Dockerfile comment).
- The entry keeps the file's `# via mooncake-transfer-engine-non-cuda` marker and
  alphabetical position (`msgpack` < `multidict`).
- The base file's single 1.2.2 hash was exactly the cp312 manylinux x86_64 wheel
  digest (`results/base-msgpack-entry.txt`), i.e. the pre-PR file followed the
  header's documented single-platform-hash convention. See F1 below.
- CI on the PR head: `kind-e2e-test` SUCCESS on all 5 matrix versions — the
  workflow builds this image (`WITH_E2E_TEST_IMAGES=true`) and runs
  `mooncake_e2e` unconditionally, so the new lock was installed and msgpack 1.2.3
  was exercised end-to-end by the PR's own CI.

## Per-finding detail

### F1 [nit] — hash-list style diverges from the file's documented convention
`test/gha-e2e/mooncake/image/requirements.txt:50`

The file header documents a regeneration procedure that yields **one
platform-matched hash per package** ("The wheels are cp312 / manylinux x86_64 …"),
and every other entry in the file has 1–2 hashes. Dependabot's replacement lists
all 91 PyPI artifacts (Windows, macOS, riscv64, pyodide/emscripten, free-threaded
wheels **plus the sdist**, which the Dockerfile's `--only-binary=:all:` can never
select).

- Functional impact: **none** (verified — C1/C2 pass; the pinned base image +
  `--only-binary` still resolve to the single intended wheel).
- Maintainability impact: a future manual regen per the documented procedure
  shrinks the entry back to one hash (churn/flip-flop); each future dependabot
  bump carries ~90 lines of noise.
- Not blocking. Options: regenerate the entry with the documented procedure after
  dependabot bumps, or update the header comment to bless the all-hashes style so
  the next regen doesn't undo dependabot's format.

## Live run notes

Not run (round policy: unit + integration only). The PR's own CI is the live
layer here and is green on the head.

## Proposed fixes (NOT applied to production here)

- No code fix required. F1 is optional cleanup by the maintainer's preference.

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/mooncake-msgpack-bump-claude` of the
reviewer's fork (`https://github.com/cheyang/fluid.git`); production code is
untouched, so it grafts onto whatever the PR head becomes.

1. Prereqs: `git`, `python3`, `pip3`, network to pypi.org (a cached
   `results/pypi-msgpack-*.json` is used as offline fallback for C1). If the host
   pip is < 24, the harness provisions a modern pip automatically when it hits
   the known vendored-urllib3 transport bug; you can also force one with
   `PIP=/path/to/pip`.
2. From any checkout that has this branch:
   `bash docs/verification/mooncake-msgpack-bump/scripts/re-verify.sh`
   — it fetches the current PR head via the manifest `pr` URL, extracts
   `test/gha-e2e/mooncake/image/requirements.txt` at that sha, and re-runs C1/C2.
3. Both claims are contract tests: **C1/C2 green = lock still sound**; red =
   lock broken/changed (or PyPI metadata changed — check `c1-detail.txt` first).
4. Harness-bites: `results/harness-bites/SUMMARY.md` shows the red-run proof; if
   a claim fails unexpectedly, re-do the bite check before trusting the verdict.
5. L3: rely on the PR's `kind-e2e-test` checks (mooncake e2e runs unconditionally
   in `gha-e2e.sh`).

### Kickoff prompt for a fresh agent
```text
Continue a verification task on branch verify/mooncake-msgpack-bump-claude of
https://github.com/cheyang/fluid.git. Background: review of
https://github.com/fluid-cloudnative/fluid/pull/6226 (msgpack 1.2.2 -> 1.2.3 in
test/gha-e2e/mooncake/image/requirements.txt) verified the lock with contract
claims C1 (hash-set == PyPI digests) and C2 (pip --require-hashes installable
for py312/manylinux x86_64). Read
docs/verification/mooncake-msgpack-bump/README.md ("Continuing after the fix")
and follow it: run scripts/re-verify.sh against the current PR head, confirm the
polarity table (both contract claims should pass), re-do the harness-bites check
if anything is red, and report an observed-vs-expected table. Do not modify
production code; push only verify/* branches to the reviewer fork.
```
