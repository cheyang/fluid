# Verification — PR #6202: bump multidict 6.7.1 → 6.9.1 in test/gha-e2e/mooncake/image

Reviewer: Reviewer B (Codex). Round 1. Harness only — no production files touched.
Branch: `verify/mooncake-multidict-bump-codex`, based on PR head `c68356d7e4d52421095a14d2fc9e5fd5fb764e64`.

## Premise (P0) — CONFIRMED

The PR is a Dependabot security update. GHSA-54p9-h82j-f925 / CVE-2026-104874
("Multidict: Reference leak in CIMultiDict/MultiDict items-view union and
subtraction", medium) covers `multidict >= 6.7.0, <= 6.9.0`; the base branch pins
`multidict==6.7.1` in `test/gha-e2e/mooncake/image/requirements.txt`, inside the
vulnerable range; 6.9.1 is the first patched version. Component match: the report
and the patch are the same file. Evidence: `results/ghsa-54p9-h82j-f925.json`,
`TestP0_BasePinsVulnerableMultidict`.

## What the harness checks

| test | layer | polarity | claim |
|---|---|---|---|
| TestP0_BasePinsVulnerableMultidict | unit | contract (premise) | base pins a vulnerable multidict |
| TestHashSetMatchesPyPI | unit | contract | PR's 171 hashes == PyPI's published 6.9.1 file set (incl. sdist) |
| TestTargetWheelHashPresent | unit | contract | the cp312/manylinux x86_64 wheel the image build needs is in the set |
| TestMultidictEntryAllPlatformHashes | unit | **canary (F1)** | entry carries 170 hashes beyond the documented one-wheel convention |
| TestPipDownloadRequiresHashesSucceeds | integration | contract | full lock file resolves + hash-checks under simulated cp312/linux x86_64 |
| TestHarnessBitesOnCorruptedHashes | integration | contract (self-check) | corrupting all multidict hashes makes pip fail |
| TestSingleHashLossFallsBackToPureWheel | integration | **canary (F1)** | losing just the cp312-wheel hash silently falls back to the pure-Python wheel |

Layers emit go-test-JSON lines so `scripts/re-verify.sh`'s gotest parser applies.
The integration layer uses `pip download --python-version 3.12 --platform
manylinux_2_28_x86_64 … --only-binary=:all: --require-hashes` because this host has
no docker / python3.12; it exercises the same wheel selection and hash
verification the image build's `pip install` performs.

## Observed vs expected (round 1, PR head c68356d7)

| finding | expected | observed | verdict |
|---|---|---|---|
| P0 premise | base pins multidict in vulnerable range | base pins 6.7.1 ∈ [6.7.0, 6.9.0] | Confirmed |
| hash set correct | set == PyPI's, target wheel hash present | symmetric diff 0; cp312 wheel hash present | Confirmed |
| build gate green | pip resolves + hash-checks the file | 17 wheels OK, cp312 C wheel selected | Confirmed |
| harness bites | corrupt hashes → pip fails | "THESE PACKAGES DO NOT MATCH THE HASHES" | Confirmed |
| F1 deviation | entry should hold 1 hash (cp312/manylinux x86_64) per file header | entry holds 171; corrupting the cp312 hash silently selects `py3-none-any` instead of failing | Confirmed (minor) |

## Raw results

- `results/pypi-multidict-6.9.1-files.json` — cached PyPI file list for 6.9.1 (fixture).
- `results/ghsa-54p9-h82j-f925.json` — cached GitHub advisory record (fixture).
- `results/integration-pip-download.log` — positive run.
- `results/integration-corrupted-hash.log` — negative control (all hashes corrupted).
- `results/integration-single-hash-loss.log` — F1 canary run (only cp312 hash corrupted).

## Re-run

```bash
# from a checkout of this branch
python3 docs/verification/mooncake-multidict-bump-codex/scripts/run_unit.py
bash    docs/verification/mooncake-multidict-bump-codex/scripts/run_integration.sh
# or, after the author pushes changes (auto-fetches current PR head):
bash    docs/verification/mooncake-multidict-bump-codex/scripts/re-verify.sh
```

## Continuing after the fix

The two F1 canaries PASS on the current PR state. If the author trims the
multidict entry back to the documented single cp312/manylinux x86_64 hash, both
canaries FLIP to fail — that flip *is* the fix signal; then invert or drop them.
The contract tests (premise, hash-set match, pip gate, harness self-check) must
stay green throughout. Handoff kickoff: "check out
verify/mooncake-multidict-bump-codex and run re-verify.sh; interpret canary
flips per docs/verification/mooncake-multidict-bump-codex/README.md".
