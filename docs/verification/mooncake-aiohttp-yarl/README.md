# Verification — PR #6212 (dependabot: aiohttp 3.14.3 → 3.14.4 in mooncake e2e image)

Harness for findings from the review of
https://github.com/fluid-cloudnative/fluid/pull/6212. Production code is
untouched; this branch adds only `docs/verification/mooncake-aiohttp-yarl/**`.

- Base: `86e3adf35a7c26875e818edad3e01dfac15f0290`
- Head under review: `ee80ce664e22d34ba62cad95c245eaa351f24f5f` (see `.last-reviewed`)

## Premise (P0) — Confirmed

The PR is a Dependabot version bump of `aiohttp` 3.14.3 → 3.14.4 in the
hash-locked requirements of the Mooncake e2e image
(`test/gha-e2e/mooncake/image/requirements.txt`, installed by the image's
Dockerfile with `pip install --only-binary=:all: --require-hashes`).
3.14.4 is a real release with real fixes (incl. a C HTTP parser segfault on
Python 3.12+, and the form-field cap that motivated raising the `yarl` floor),
so bumping is legitimate. The component matches: the PR touches exactly the
file that pins aiohttp for this image. The question F1 answers is whether the
bump as generated keeps the lock file installable — it does not.

## Findings

| ID | Claim | Polarity | Layer | Result on head |
|----|-------|----------|-------|----------------|
| F1 | The bumped lock file no longer resolves: aiohttp 3.14.4 requires `yarl<2.0,>=1.25.1` (3.14.3 required `>=1.17.0`), but the PR leaves `yarl==1.24.5` pinned, so the image build's `pip install --require-hashes` fails with `ResolutionImpossible` and all five `kind-e2e-test` jobs go red | contract | unit (pip dry-run for cp312/manylinux x86_64) | **Confirmed — fails as predicted** |

## How to run

From the repo root of this branch (needs python3+pip and network to a PyPI
index carrying `mooncake-transfer-engine-non-cuda`; default pypi.org works,
override with `PIP_INDEX_URL`):

```bash
bash docs/verification/mooncake-aiohttp-yarl/scripts/check-requirements-resolution.sh
# prints: MooncakeRequirementsResolve<TAB>pass|fail ; exit 0 iff pass
```

This simulates the Dockerfile's resolution for the image's platform
(CPython 3.12 / manylinux x86_64) via pip's `--python-version/--platform/--abi`
flags, so no docker daemon is needed. It exercises the full requirements file
with `--require-hashes --only-binary=:all:`, exactly as the image build does.

To re-verify a later PR head (or any ref):

```bash
bash docs/verification/mooncake-aiohttp-yarl/scripts/re-verify.sh        # auto-fetches current PR head
bash docs/verification/mooncake-aiohttp-yarl/scripts/re-verify.sh <sha>  # explicit ref
```

## Observed vs expected

| Run | Expected | Observed | Evidence |
|-----|----------|----------|----------|
| base file (86e3adf) | resolves | exit 0, "Would install aiohttp-3.14.3 ... yarl-1.24.5" | `results/base-dryrun.log` |
| PR head file (ee80ce6) | resolves | **exit 1, `ResolutionImpossible: aiohttp 3.14.4 depends on yarl<2.0,>=1.25.1` vs `yarl==1.24.5`** | `results/pr-head-dryrun.log` |
| live CI (kind-e2e-test v1.22.17–v1.33.2, run 39403532072) | image builds | all 5 jobs fail at the Dockerfile pip step with the identical error | `results/ci-failure-excerpt.log` |
| head + proposed fix (yarl→1.25.1 + hash) | resolves | exit 0, "Would install ... aiohttp-3.14.4 ... yarl-1.25.1" (harness-bites check) | `results/fixed-dryrun.log` |

Secondary checks: all 160 aiohttp hashes in the PR are genuine PyPI 3.14.4
artifacts and include the needed cp312 manylinux x86_64 wheel; every other
aiohttp 3.14.4 dependency floor is satisfied by the existing pins
(aiohappyeyeballs 2.7.1, aiosignal 1.4.0, attrs 26.1.0, frozenlist 1.8.0,
multidict 6.9.1, propcache 0.5.2, typing_extensions 4.16.0). yarl is the only
broken floor.

## Proposed fix (verified)

In `test/gha-e2e/mooncake/image/requirements.txt`, bump the yarl entry:

```
yarl==1.25.1 \
    --hash=sha256:c6f117789d22dce188e5754e8bc65b7e6ebf8cb73963b9fa761f672a5883769d
```

(hash = `yarl-1.25.1-cp312-cp312-manylinux2014_x86_64.manylinux_2_17_x86_64.manylinux_2_28_x86_64.whl`
from PyPI; yarl 1.25.1 needs `idna>=2.0`, `multidict>=4.0`, `propcache>=0.2.1`,
all satisfied by the existing pins.) The file header documents the docker-based
regeneration procedure; running it would have produced this entry.

## Continuing after the fix

1. `git fetch` this branch, check it out.
2. Run `scripts/re-verify.sh` — it fetches the current PR head, grafts this
   harness, and prints `F1 contract FIXED/STILL-BROKEN`. Contract polarity:
   pass = fixed.
3. Live layer (not run here, by round policy): the `kind-e2e-test` workflow
   itself — it docker-builds the image and runs the mooncake e2e.

Kickoff prompt for a fresh agent: "Check out branch
verify/mooncake-aiohttp-yarl-codex of the fluid review fork and run
docs/verification/mooncake-aiohttp-yarl/scripts/re-verify.sh; report F1 per
the README table."
