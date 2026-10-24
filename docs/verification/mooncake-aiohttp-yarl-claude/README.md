# Verification — mooncake e2e image lock broken by the aiohttp 3.14.4 bump (PR #6212)

- **PR:** https://github.com/fluid-cloudnative/fluid/pull/6212 (bump aiohttp 3.14.3 → 3.14.4 in `test/gha-e2e/mooncake/image/requirements.txt`)
- **Head / base:** `ee80ce66` / `86e3adf3`
- **Harness branch:** `verify/mooncake-aiohttp-yarl-claude` (reviewer fork). Production code is untouched — the diff against the PR head is this directory only.

## Premise (P0) — verdict: Confirmed

The PR is a routine dependabot maintenance bump of a hash-locked pin in the
mooncake e2e helper image. The base file pins `aiohttp==3.14.3`; 3.14.4 exists
on PyPI and ships features plus ~20 bug fixes (including HTTP/WebSocket parser
hardening). Component matches (the file the PR touches is the file that pins
aiohttp for that image). The premise is real and trivially so; **the problem is
not the premise, it is that the bump breaks the lock** (see F1).

## F1 [blocker] — the lock no longer resolves; the e2e image build fails

**Claim.** With `aiohttp==3.14.4` pinned but `yarl==1.24.5` left untouched, the
Dockerfile's `pip install --only-binary=:all: --require-hashes -r requirements.txt`
fails with `ResolutionImpossible`, because aiohttp 3.14.4's wheel metadata
requires `yarl<2.0,>=1.25.1` (3.14.3 required only `>=1.17.0`). The
`E2E Functionality Check` workflow builds this image on every PR
(`WITH_E2E_TEST_IMAGES=true`, `docker build test/gha-e2e/mooncake/image`), so
the whole 5-version kind-e2e matrix goes red.

**Observed vs expected.**

| run | layer | result | evidence |
| --- | --- | --- | --- |
| PR head `ee80ce66` | unit (lock vs PyPI metadata) | `deps-consistent: fail` — `aiohttp==3.14.4 requires yarl<2.0,>=1.25.1 but the lock pins yarl==1.24.5` | `results/head-unit.txt` |
| PR head `ee80ce66` | integration (pip resolver, exact Dockerfile flags, cp312/manylinux x86_64) | `lockfile-resolves: fail` — `ResolutionImpossible` | `results/head-pip-download.log` |
| base `86e3adf3` (control) | both | `pass` / `pass` | `results/base-control-*` |
| CI (kind-e2e, all 5 jobs) | real docker build | FAILURE — same `ResolutionImpossible` | `results/ci-kind-e2e-failure-excerpt.txt` (run 39403532072) |
| head + proposed fix (yarl→1.25.1) | both | `pass` / `pass` — harness bites | `results/fixcheck-*` |

The CI attribution is differential, not just correlational: run 39403532072 is
the **only** failing `E2E Functionality Check` run in the recent history —
neighbouring dependabot bumps to the *same file* (e.g. the certifi bump,
run 39525525145) and regular PRs are green, and the only delta is this PR's
aiohttp entry.

**Proposed fix.** Bump the yarl entry too, e.g.

```
yarl==1.25.1 \
    --hash=sha256:c6f117789d22dce188e5754e8bc65b7e6ebf8cb73963b9fa761f672a5883769d
```

(or regenerate the whole file with the procedure documented at the top of
`requirements.txt`). yarl 1.25.1's own deps (`idna>=2.0`, `multidict>=4.0`,
`propcache>=0.2.1`) are satisfied by the existing pins, so the single-entry
bump is sufficient — verified by `results/fixcheck-*`. Longer term, dependabot
does not re-resolve the other pins in a hash-locked file, so any aiohttp bump
that changes transitive constraints needs this manual step (the file header's
regeneration procedure exists precisely for this).

## How to run

From a checkout of this branch at the repo root:

```bash
# unit layer: lock vs live PyPI metadata (fast, no downloads)
python3 -I docs/verification/mooncake-aiohttp-yarl-claude/scripts/check-lockfile-consistency.py \
    test/gha-e2e/mooncake/image/requirements.txt

# integration layer: the Dockerfile's pip step, exactly, for cp312/manylinux x86_64
bash docs/verification/mooncake-aiohttp-yarl-claude/scripts/check-lockfile-resolves.sh \
    test/gha-e2e/mooncake/image/requirements.txt

# both + per-finding verdict (auto-fetches the current PR head)
bash docs/verification/mooncake-aiohttp-yarl-claude/scripts/re-verify.sh
```

Requires: git, jq, python3, network to pypi.org. The integration script pins
`--index-url https://pypi.org/simple` (host pip configs pointing at partial
mirrors have been observed to break these fetches) and self-upgrades to a
modern pip in a scratch venv when the host pip is older than 24.

## Polarity

Both checks are **contract** tests: green = the lock resolves for the image's
platform, red = the image build fails. No bug-canaries in this harness.

## Continuing after the fix

`re-verify.sh` with no argument resolves the current PR head, grafts this
harness onto it and prints F1 as **FIXED** only when `lockfile-resolves` goes
green on the new head; exit 0 iff all findings are fixed. If the author
regenerates the whole lock instead of bumping yarl alone, the unit layer's
`wheel-coverage` / `deps-consistent` checks still apply unchanged (Harness-update
only if the requirements file moves or the format changes).

## Kickoff prompt for a fresh agent

> You are resuming a PR review verification. Repo: fluid-cloudnative/fluid, PR #6212.
> Check out branch `verify/mooncake-aiohttp-yarl-claude` (reviewer fork
> https://github.com/cheyang/fluid.git), read
> docs/verification/mooncake-aiohttp-yarl-claude/README.md, then run
> `bash docs/verification/mooncake-aiohttp-yarl-claude/scripts/re-verify.sh`
> (no args — it fetches the current PR head). Report per finding
> Fixed / Still-broken / Partial / Harness-update, then advance
> `.last-reviewed` to the head you verified and push the branch.
