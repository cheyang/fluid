# Verification — PR #6225: ci: pin kind-e2e runner to ubuntu-24.04

- PR: https://github.com/fluid-cloudnative/fluid/pull/6225
- Reviewed head: `fa06ee36533c8da2b68023c6bcea436e530fb540` (base `fe074af38434fc4b618686de6459a9173570b48b`)
- Reviewer: Codex (Reviewer B), round: initial
- Production code touched by this harness: **none** (additive `docs/verification/**` only)

## Premise (P0) — CONFIRMED

Claim (PR body / issue #6224): `ubuntu-latest` now resolves to the `ubuntu-26.04`
runner image, where all 5 `kind-e2e-test` matrix jobs fail because the
JindoRuntime FUSE container's mount of `/jfs/jindofs-fuse` is denied
(`fusermount3: mount failed: Permission denied`); Ubuntu 24.04 runners do not
exhibit this.

Verified against the base state (the premise runs *against the base*, i.e. the
un-pinned workflow), using GitHub Actions evidence only — no cluster needed:

| what | observed | evidence |
|---|---|---|
| Failing run 40474966878 (PR #6219), label `ubuntu-latest` | job ran on **Image: ubuntu-26.04** (Version 20261012.163.1); all 5 matrix jobs failed | `results/fail-run-40474966878/runner-image.txt` |
| Failure mode | `jindo-demo-jindofs-fuse` pod CrashLoopBackOff; `jindoruntime secret e2e` fails (`warmup pod jindo-demo-warmup is not ready`) | `results/fail-run-40474966878/failure-symptoms.txt` |
| Fuse container log (uploaded diagnose artifact of that run) | `FLUID_FUSE_MOUNTPOINT = /jfs/jindofs-fuse` … `fusermount3: mount failed: Permission denied` | `results/fail-run-40474966878/fuse-error-excerpt.txt`, `jindofs-fuse-pod.log` |
| Same PR's earlier run 40125302787, label `ubuntu-latest` | ran on **Image: ubuntu-24.04** (20261012.334.1); all 5 passed | `results/pass-run-40125302787/runner-image.txt` |
| This PR's own run 40885813005 at head `fa06ee36` | all 5 `kind-e2e-test` jobs `success` on label `ubuntu-24.04` | `results/pr-head-run-40885813005/jobs.txt` |
| Scope: other workflows left on `ubuntu-latest` | `backward-compatibility-e2e.yml` only exercises the Alluxio path (`test/gha-e2e/alluxio/{dataset,job}.yaml`), no Jindo FUSE — consistent with it passing on 26.04; `project-check`/`codeql`/`pr-quota-limit`/`sonarcloud` run no FUSE workloads | repo: `.github/scripts/gha-backward-compatibility.sh` |

Note: at review time (2026-10-30) the actions/runner-images README table still
lists `ubuntu-latest` = Ubuntu 24.04 while 26.04 migration is in its gradual
rollout window — i.e. `ubuntu-latest` resolves to 26.04 for a growing share of
jobs, which matches the observed fail-on-26.04/pass-on-24.04 flakiness under the
same label. The pin removes that nondeterminism.

The AppArmor-5.0.2-mechanism part of the root cause (profile
`/etc/apparmor.d/fusermount3` allows mounts only under $HOME//mnt//run/user/
/media//tmp//cvmfs/) is tracked in issue #6224, which itself lists kernel-audit
confirmation as a follow-up. The mitigation this PR ships does not depend on
that mechanism being fully proven: the fail/pass correlation across runner
images is directly observed (above), and the pinned run is green.

## Findings verified this round

No defects found in the diff. The contract checks below guard the mitigation
itself (so a later revision that silently drops the pin is caught).

| id | claim | layer | polarity | result | evidence |
|---|---|---|---|---|---|
| C1 | `kind-e2e-test` job runs on a pinned `ubuntu-2x.04` label, not floating `ubuntu-latest` | unit (static) | contract | pass at head; fails on base workflow (harness bites) | `results/unit.out` |
| C2 | no other job inside `kind-e2e.yml` floats on `ubuntu-latest` | unit (static) | contract | pass (workflow has exactly one job) | `results/unit.out` |
| C3 | the pin carries `TODO(#6224)` pointing at the tracking issue | unit (static) | contract | pass | `results/unit.out` |
| C4 | tracking issue #6224 is open | unit (API) | contract | pass (state=open, labels bug/Jindo) | `results/unit.out` |

Harness-bites check: `check-pin.sh` against the **base** workflow file fails C1
and C3 (exit 1) and passes at the head (exit 0).

## How to re-run

```bash
# static contract checks against the checked-out tree
bash docs/verification/pr-6225-kind-e2e-ubuntu-pin/scripts/check-pin.sh

# full re-verify against the CURRENT PR head (fetches via manifest.pr)
bash docs/verification/pr-6225-kind-e2e-ubuntu-pin/scripts/re-verify.sh
```

Live layer: intentionally skipped (CI-workflow-only change; the PR's own
Actions run is the live evidence). After the real fix for #6224 lands, the
signal to check is the E2E Functionality Check passing on an `ubuntu-26.04`
runner with no `fusermount3` EACCES in the jindofs-fuse pod log.

## Continuing after a fix / next round

- This is the regression suite for future rounds of PR #6225: re-run
  `scripts/re-verify.sh`; contract checks must stay green. If a future revision
  legitimately returns to `ubuntu-latest` because #6224 was fixed, invert C1's
  expectation (it becomes a canary) rather than deleting it.
- `.last-reviewed` holds `fa06ee36533c8da2b68023c6bcea436e530fb540`; the next
  round reviews `<.last-reviewed>..head` and advances the marker.
