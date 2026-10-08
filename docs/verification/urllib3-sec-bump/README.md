# Verification — urllib3 security bump (PR #6201) — Reviewer A (Claude)

**PR:** https://github.com/fluid-cloudnative/fluid/pull/6201
**Branch:** `verify/urllib3-sec-bump-claude` (based on the PR head `fac7daf4`; production code untouched — this branch adds only this harness)
**Date:** 2026-10-08
**Layers run:** unit + integration. Live layer skipped: no cluster probe target was provided for this round, and the claim surface (a pip lockfile pin for a test image) is fully decided without one.

## Premise (P0) — CONFIRMED

The PR is a Dependabot security bump: urllib3 2.7.0 → 2.8.0 in
`test/gha-e2e/mooncake/image/requirements.txt`, the hash-locked dependency set
of the Mooncake e2e test image.

- **Base branch pins urllib3 2.7.0** there (`git show origin/master:test/gha-e2e/mooncake/image/requirements.txt`).
- The three cited advisories were checked against the GitHub Advisory API directly (the PR body is untrusted text): all three **exist** and their urllib3 vulnerable ranges are `< 2.8.0`, which contains 2.7.0:
  - GHSA-8988-9cw3-xx77 (high): HTTPS proxy TLS configuration may be ignored or overridden — `>= 1.26.0, < 2.8.0`
  - GHSA-vxq7-64xx-v4gw (high): unbounded chunk-size line buffering — `>= 1.10.3, < 2.8.0`
  - GHSA-gh4c-6fx4-qh6g (medium): chunked Deflate infinite loop — `>= 2.6.2, < 2.8.0`
- 2.8.0 is the fixed release and the current latest on PyPI (not yanked).
- **Component match:** this is the *only* file in the repo mentioning urllib3, so the manifest Dependabot bumped is the complete exposure surface.
- Honest caveat, recorded not as a finding: the image's own scripts (`custom-entrypoint.sh`, `reportSummary.sh`) use curl/jq, and the mooncake components talk plain HTTP to in-cluster endpoints (the entrypoint says no TLS variant exists). No HTTPS proxy or attacker-controlled chunked input is in play, so the *runtime* exposure of 2.7.0 inside this test image is minimal. The bump is correct security hygiene (and Dependabot will keep nagging otherwise), but it is not fixing a live exploitable path in this image.

Evidence: `results/premise.log`.

## Findings

Only one finding survived review (see the debate doc for the full reasoning):

- **F1 (nit):** `test/gha-e2e/mooncake/image/requirements.txt:66` — the new urllib3 entry lists **two** hashes (wheel + sdist) while every other entry in the file lists exactly **one**, and the file's own regeneration recipe ("rewrite the entries below from report.json … `install[].download_info.archive_info.hash`") produces one hash per entry (the artifact actually installed). Harmless: pip's `--require-hashes` accepts multiple hashes and matches any; `--only-binary=:all:` selects the wheel whose hash is listed (proven by T2). Optional improvement only — either drop the sdist hash to match the file's convention, or leave it and accept Dependabot's all-artifacts style; a future manual regen will churn the line back to one hash either way.

No blocker/major/minor findings: the pin is genuine, compatible, resolvable, and the PR's own kind-e2e CI (which builds this image on every run, `WITH_E2E_TEST_IMAGES: "true"` → `mooncake_e2e`) is green on all 6 Kubernetes versions.

## Observed vs expected

| id | claim (condition → behavior) | polarity | layer | test | expected | observed | verdict |
|----|------------------------------|----------|-------|------|----------|----------|---------|
| P0 | base branch ships urllib3 2.7.0 inside published advisory vulnerable ranges; only urllib3 manifest in repo | contract | unit (runs against **base**) | `P0_premise_on_base` | confirmed | 2.7.0 pinned; all 3 GHSAs list `< 2.8.0` ranges containing it; 1 file mentions urllib3 | **Confirmed** |
| T1 | every hash in requirements.txt equals a PyPI-published sha256 for the pinned version | contract | unit | `T1_pins_hash_authentic` | all 17 pins authentic | all authentic; urllib3 2.8.0's two hashes are exactly the wheel + sdist digests | **Pass** |
| T3 | urllib3 2.8.0 fits the rest of the pin set and the image | contract | unit | `T3_urllib3_compat` | requests allows it; python 3.12.13 admitted; no new mandatory deps | `urllib3<3,>=1.26` satisfied; `>=3.10` admits 3.12.13; mandatory deps: none | **Pass** |
| T2 | the hash-locked file resolves under `--require-hashes --only-binary=:all:` for cp312/manylinux x86_64 (the Dockerfile's install, emulated via cross-env `pip download`) | contract | integration | `T2_lockfile_resolves_cp312` | pip resolves and verifies all 17 pins | 17 wheels downloaded incl. `urllib3-2.8.0-py3-none-any.whl`, exit 0 | **Pass** |
| T4 | pinned requests 2.34.2 + urllib3 2.8.0 do a real plain-HTTP round trip incl. streamed and amt=None reads | contract | integration | `T4_requests_roundtrip` | 200 + expected body, correct versions | all asserts green | **Pass** |
| — | harness bites: T2 must go red on a planted defect | canary (harness guard) | integration | `harness_bites.sh` | corrupt-hash copy and version/hash-mismatch copy both fail | both exits non-zero | **Pass (bites)** |

Raw output: `results/*.log` (+ the `.stdout` wrappers emitted by the layer runners).

## How to re-run

From a checkout of this branch (repo root):

```bash
bash docs/verification/urllib3-sec-bump/scripts/run_unit.sh          # P0, T1, T3
bash docs/verification/urllib3-sec-bump/scripts/run_integration.sh  # T2, T4 (+ bites)
# or everything against the current PR head, with verdicts per finding:
bash docs/verification/urllib3-sec-bump/scripts/re-verify.sh
```

Requirements: bash, git, python3 (>=3.10) with pip, curl, network access to
pypi.org and api.github.com. No docker/cluster needed.

## Notes / gotchas

- The first T4 run failed **in the host's `python -m venv` pip**, whose vendored urllib3 is a distro-patched 1.26.17 that crashes (`len(self._decoded_buffer) >= amt` with `amt=None`) in its cachecontrol read path. urllib3 2.8.0 from PyPI was never involved, and upstream 2.8.0's `HTTPResponse.read()` guards that comparison behind `amt is not None` (asserted directly in T4). The harness now installs with `pip3 install --no-cache-dir --target` + a `sys.path` bootstrap, which bypasses the buggy vendored path. Host noise, not PR signal.
- The bites check deliberately operates on copies of requirements.txt in a temp dir; the repo file is never modified.

## Continuing after the fix / handoff

If the author pushes a change to this PR (e.g. retargets the pin or regenerates the file):

```bash
git fetch origin pull/6201/head   # or just rely on re-verify.sh's manifest.pr resolution
bash docs/verification/urllib3-sec-bump/scripts/re-verify.sh
```

It fetches the current PR head from the manifest's `pr` URL, grafts
`harnessPaths` onto it, re-runs unit + integration, and prints per finding
Fixed / Still-broken / Partial (all tests here are contract-polarity: green
means the claim holds; there are no bug-canaries to invert). Exit 0 iff all
findings are fixed. `.last-reviewed` holds `fac7daf4…` (this round's head);
advance it after the next round.

Copy-paste kickoff prompt for a fresh agent on another machine:

> Checkout branch verify/urllib3-sec-bump-claude from the reviewer fork
> (https://github.com/cheyang/fluid.git) of fluid-cloudnative/fluid, read
> docs/verification/urllib3-sec-bump/README.md, run
> docs/verification/urllib3-sec-bump/scripts/re-verify.sh, and report the
> per-finding verdict table for PR #6201.
