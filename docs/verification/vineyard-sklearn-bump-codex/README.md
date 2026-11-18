# vineyard-sklearn-bump-codex — verification of PR fluid-cloudnative/fluid#6260

Reproducible evidence for the review of
<https://github.com/fluid-cloudnative/fluid/pull/6260>
("chore(deps): bump scikit-learn from 1.4.0 to 1.5.0 in /samples/vineyard",
Dependabot security update, single file: `samples/vineyard/requirements.txt`).

Reviewed head: `e782cc7d3ea2c69c123851cc0b24e78b5414cfac` (base `1321a217d78ab99c0cb76d2ea334d593a3765509`).

Layers run against the PR head (and re-run against the base as control):

| Layer | What it exercises | How to run (from repo root) |
|-------|-------------------|-----------------------------|
| 1. Unit | advisory existence (P0), hash set vs PyPI (C1), header/pin consistency (F1) | `bash docs/verification/vineyard-sklearn-bump-codex/scripts/run_unit_layer.sh` |
| 2. Integration | real `pip install` with the Dockerfile's exact flags in a python 3.10 venv (C2a), cp310 manylinux amd64/arm64 wheel resolution + hash verification (C2b/C2c), the real sample scripts end-to-end (C3), `import vineyard` canary for pre-existing breakage (C4) | `bash docs/verification/vineyard-sklearn-bump-codex/scripts/run_integration_layer.sh` (needs `uv` on PATH and network) |
| 3. Live | skipped: change only affects manually built sample images; no cluster behavior. The with_vineyard=True path needs a live vineyard server and is already broken at base (see C4). | — |

> Test polarity: all checks are contract tests (FAIL = problem present). C4 is a
> contract test that fails on **both** base and head: it documents a pre-existing
> breakage this PR neither causes nor fixes. F1 fails on head, passes on base:
> introduced by this PR.

## Problem premise (P0)

| | |
|---|---|
| Claimed symptom | Dependabot body: "Bumps scikit-learn from 1.4.0 to 1.5.0". No `fixes #N`. The repo's `.github/dependabot.yml` configures **no pip ecosystem**, so a pip Dependabot PR can only be a security update. |
| Advisory | GHSA-jw8x-6495-233v / CVE-2024-5206, `pip:scikit-learn`, vulnerable `< 1.5.0`, first patched `1.5.0`, severity medium (GitHub Advisory API, `results/head/P0.log`) |
| Reported component | scikit-learn pinned in `samples/vineyard/requirements.txt` |
| Patched component | exactly that pin (+ its hash block) |
| Component match | Yes |
| **Verdict** | **Confirmed** — the advisory exists and covers 1.4.0; 1.5.0 is the fix. Caveat: the vulnerable component (`TfidfVectorizer`) is not used by the sample scripts, so for these images it is hygiene rather than an active exploit fix. |

## Summary of results

| ID | Claim | Layer | Base (1.4.0) | Head (1.5.0) | Verdict |
|----|-------|-------|--------------|--------------|---------|
| P0 | advisory motivates the bump | 1 | — | pass | Confirmed |
| C1 | sklearn hash block == PyPI digest set, cp310 amd64+arm64 wheels covered | 1 | pass (41 files) | pass (21 files) | Holds |
| C2 | Dockerfile pip command resolves hash-locked on py3.10; wheels verify for amd64/arm64 | 2 | pass | pass | Holds |
| C3 | sample pipeline (preprocess→train→test) runs on the pinned env | 2 | pass, MSE 45484148750.70735 | pass, MSE identical | Holds — no behavior change |
| F1 | header regeneration command pins the version the file pins | 1 | pass | **fail** (documents 1.4.0, pins 1.5.0) | **Open finding (minor), introduced by this PR** |
| C4 | `import vineyard` works in the pinned env | 2 | fail | fail | Pre-existing breakage, NOT this PR's doing |

Raw logs: `results/head/*.log`, `results/base/*.log`.

## Per-finding detail

### C1 — hash completeness/correctness (acceptance)
`scripts/check_hashes.py` parses the `scikit-learn` entry (joining `\` continuations),
fetches `https://pypi.org/pypi/scikit-learn/<version>/json`, and compares digest sets.
Head: 21 hashes == PyPI's 21 files for 1.5.0; includes
`scikit_learn-1.5.0-cp310-cp310-manylinux_2_17_x86_64...whl` and `..._aarch64...whl`.
The old entry's 41 hashes match 1.4.0's 41 files (1.4.0 shipped two wheel builds),
so the 41→21 reduction is upstream publishing fewer files, not lost coverage.
Bites check: flipping one hex char makes the checker exit 1 with MISSING/EXTRA diagnostics.

### C2 — pip resolution with the Dockerfile's exact flags (acceptance)
`uv venv --seed --python 3.10`, then verbatim
`pip install --no-cache-dir --only-binary=:all: --no-binary=lazy-import --require-hashes -r requirements.txt`.
Head: all 28 packages install, `pip check` clean, `sklearn=1.5.0 numpy=1.26.4` (`results/head/C2a.log`).
C2b/C2c: `pip download --no-deps --require-hashes --only-binary=:all: --python-version 3.10 --implementation cp --platform manylinux_2_17/2014/2_28_<arch>`
for the isolated sklearn block — the cp310 manylinux x86_64 and aarch64 wheels
download and hash-verify (`results/head/C2-amd64.log`, `results/head/C2-arm64.log`).
lazy-import is excluded from the emulated runs only (sdist-only; pip cannot do
platform emulation for sdists; it is unchanged by this PR and is exercised
natively in C2a).

### C3 — sample code compatibility (acceptance)
Real `preprocess-data.py` → `train-data.py` → `test-data.py` from the PR tree,
run in the C2a environment; `prepare-data.py`'s real `generate_random_dataframe`
is imported to produce a 2000-row fixture (skipping its 30M-row generation and
its terminal `time.sleep`). Only edit: `/data` paths sed-rewritten to the sandbox,
and a stub `vineyard` module shadows the real one (the with_vineyard=False path
never calls it; the real import is broken at base — see C4). The scripts are run
without `--with_vineyard` (the scripts parse it with `type=bool`, so an explicit
"False" string is truthy — a pre-existing sample quirk the stub caught).
Head and base both pass with **bit-identical** MSE 45484148750.70735: the bump
does not change the sample's numerical output.

### F1 — stale regeneration command in the file header (finding, minor)
The header documents: `printf "pandas\nrequests\nnumpy\nvineyard\nscikit-learn==1.4.0\njoblib==1.3.2\n" | uv pip compile ...`
After this bump, following the file's own documented procedure regenerates the
file with scikit-learn **1.4.0**, silently reverting the security update.
Dependabot edits the pin+hashes but not comments. Differential:
`check_header_consistency.py` passes on base, fails on head (`results/head/F1.log`).
Fix: update the version literal in the header (or make the command read the pin
from the file). Not a build/runtime defect — severity minor.

### C4 — pre-existing: `import vineyard` broken in the pinned env (NOT this PR)
`requirements.txt` (since #6257, the base commit) pins `setuptools==84.2.1`, which
no longer ships `pkg_resources`; `vineyard 0.24.2` does
`from vineyard.deploy.utils import ...` → `import pkg_resources` →
ModuleNotFoundError. All three pipeline stages import vineyard at module top, so
the sample images fail at startup. Fails identically on base and head
(`results/base/C4.log`, `results/head/C4.log`); introduced by #6257's lockfile,
not by this bump. Parked as pre-existing; worth a separate issue (pin
`setuptools<81` for the sample or wait for a vineyard release without
pkg_resources).

## Continuing after the fix (possibly on another machine)

Branch `verify/vineyard-sklearn-bump-codex` (production code untouched; diff vs
the PR head is only this directory).

```bash
git fetch https://github.com/cheyang/fluid.git verify/vineyard-sklearn-bump-codex
git checkout verify/vineyard-sklearn-bump-codex
bash docs/verification/vineyard-sklearn-bump-codex/scripts/re-verify.sh   # auto-fetches the current PR head
```

Prerequisites: python3, curl, `uv` (`curl -LsSf https://astral.sh/uv/install.sh | sh`),
network access to PyPI. Layers: unit = P0/C1/F1; integration = C2/C3/C4 (downloads
~700 MB into a temp venv; honors `UV_CACHE_DIR`). Expected after the author fixes
F1 (header literal): `F1` flips to pass; C1/C2/C3 stay green; C4 stays red until
the sample's setuptools/vineyard combination is fixed separately.

Kickoff prompt for a fresh agent: "Check out branch verify/vineyard-sklearn-bump-codex
from https://github.com/cheyang/fluid.git, read docs/verification/vineyard-sklearn-bump-codex/README.md,
run scripts/re-verify.sh, and report per finding Fixed / Still-broken."
