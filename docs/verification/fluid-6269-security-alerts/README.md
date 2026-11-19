# Verification — PR #6269 (fluid) — security alert cleanup

Reviewer A (Claude), round 1, 2026-11-19.
Head: `7ec63f64207653baeec60fb60b1a05ec02aaf4f8` — base: `dcb76167b0143a239addc6bed83ed50b45267631`.
Branch: `verify/fluid-6269-security-alerts-claude` on `cheyang/fluid`. Production code untouched
(only this harness + `hack/sdk/post_gen_path_test.py`, additive).

## Premise (P0) — CONFIRMED

The PR clears the repo's remaining security alerts. All four sub-claims reproduce:

| claim | evidence | verdict |
|---|---|---|
| 17 open aiohttp Dependabot alerts (82–101) in the mooncake lock; all ranges `<=3.14.3` or `<3.14.4` | `gh api` listing (18 open Dependabot total: 17 aiohttp + 102 sklearn) | Confirmed |
| Dependabot #6212 (aiohttp-only bump) can never build: aiohttp ≥3.14.4 requires `yarl>=1.25.1` | PyPI metadata for 3.14.4/3.14.5 (`yarl<2.0,>=1.25.1`); canary fixture reproduces the violation — `results/canary-check.txt` | Confirmed |
| scikit-learn 1.4.0 → 1.7.2 clears Dependabot 102 (CVE-2024-5206, fixed in 1.5.0); 1.7.x is the newest series supporting the image's Python 3.10 | PyPI `requires_python`: 1.7.2 `>=3.10`, 1.8.0/1.9.0 `>=3.11` | Confirmed |
| post-gen.py alerts 1481/1482 are `pythonsecurity:S8707` "Path Traversal via faulty LLM-supplied CLI arguments"; the allow-list pattern matches the one that cleared `mergeByTOC.py` (parse_lang, `docs/scripts/mergeByTOC.py:18`) | `gh api` code-scanning listing (16 open: 2 post-gen.py + 14 under `sdk/python`) | Confirmed |
| submodule b06de4e8 is the merge of fluid-client-python#12, its master HEAD, only that PR's 8 files changed | `scripts/check-submodule.sh` → `results/submodule-check.txt` | Confirmed |

## Observed vs expected

| finding / claim | expected | observed | verdict |
|---|---|---|---|
| F1 post-gen.py allow-list behavior (no committed test in PR) | harness 10/10 pass on head | 10/10 — `results/post-gen-head.txt` | Confirmed (harness supplied) |
| F1 bite-check on pre-PR script | rejection + decoy tests fail on base | decoy-dir test fails on base with "decoy dir accepted" — the pre-PR script **rewrote an in-repo decoy dir**; new script refuses — `results/post-gen-bite-check-base.txt` | Bites |
| Mooncake lock (cp3.12) | all hashes match PyPI digests; all pins installable cp312 manylinux x86_64; all dep edges satisfied | 17 pins / 20 edges / 0 errors — `results/mooncake-lock-report.json` | Pass |
| Vineyard lock (cp3.10) | same for cp3.10 | 28 pins / 30 edges / 0 errors; sklearn 1.7.2 hashes cover all 31 PyPI files incl. aarch64 — `results/vineyard-lock-report.json` | Pass |
| Canary: aiohttp-only bump (#6212 shape) | unresolvable (yarl constraint) | exit 1: `aiohttp==3.14.4 requires yarl<2.0,>=1.25.1 but lock pins yarl==1.24.5` — `results/canary-report.json` | Pass (premise holds) |
| Submodule bump identity | gitlink = b06de4e8, merge commit, 8 files, master HEAD | all match — `results/submodule-check.txt` | Pass |
| CI (kind-e2e on head) | builds mooncake image from new lock | kind-e2e-test green on 5 k8s versions | Pass |

L3 live layer skipped (no KUBECONFIG per debate setup). kind-e2e already builds the mooncake
image with `--require-hashes` on the PR head, which covers the lock change end-to-end.

## How to run

```bash
bash docs/verification/fluid-6269-security-alerts/scripts/re-verify.sh   # current PR head
bash docs/verification/fluid-6269-security-alerts/scripts/re-verify.sh <ref>
python3 hack/sdk/post_gen_path_test.py                                    # L1 alone
```

`check_lock.py` needs the `packaging` module (auto-discovered from user site).
`check-submodule.sh` and the canary need network (PyPI / GitHub).

## Findings

- **F1 (minor, open)**: no committed test for the post-gen.py allow-list change. This branch's
  `hack/sdk/post_gen_path_test.py` is the coverage and can be uplifted post-merge.
- **F2 (nit, open)**: `PYTHON_SDK_DIRS` (post-gen.py) duplicates `PYTHON_SDK_OUTPUT_PATH`
  (gen-sdk.sh); drift fails loudly (SystemExit), acknowledged in the PR notes.

No blockers, no majors: the premise is confirmed, both lock files verify hash-by-hash against
PyPI with all dependency constraints satisfied on the target platforms, the submodule bump is
exactly what it claims, and the post-gen.py hardening demonstrably closes the S8707 finding
(the pre-PR script rewrote an in-repo decoy directory; the new one refuses).

## Continuing after the fix

If the PR is updated: re-run `scripts/re-verify.sh` (fetches the new head, grafts this harness,
prints per-claim verdicts). All tests here are **contract** tests: green = correct behavior.
The only canary is the lock premise check (expected to *fail* while aiohttp≥3.14.4 requires
yarl≥1.25.1 — i.e. Dependabot-style aiohttp-only bumps remain unbuildable); if it ever flips
to resolvable, the premise changed and should be re-examined, not celebrated.

Copy-paste kickoff for a fresh agent:

> You are resuming a review verification for fluid PR #6269. Checkout branch
> `verify/fluid-6269-security-alerts-claude` (fork cheyang/fluid), read
> `docs/verification/fluid-6269-security-alerts/README.md` and `verify-manifest.json`,
> run `bash docs/verification/fluid-6269-security-alerts/scripts/re-verify.sh` (no args
> resolves the current PR head), then review the `last-reviewed..head` delta and update
> this README, the manifest and `.last-reviewed`.
