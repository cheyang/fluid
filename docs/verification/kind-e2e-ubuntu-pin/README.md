# kind-e2e-ubuntu-pin — verification harness

Reproducible evidence for the review of [PR #6225](https://github.com/fluid-cloudnative/fluid/pull/6225)
("ci: pin kind-e2e runner to ubuntu-24.04"), head `fa06ee36`, base `fe074af38`.

The PR is a 2-line CI-config change: `jobs.kind-e2e-test.runs-on` in
`.github/workflows/kind-e2e.yml` goes from `ubuntu-latest` to `ubuntu-24.04`, plus a
`TODO(#6224)` marker. No production code is touched, and this harness adds no production
changes either — the diff on this branch is harness-only.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (deterministic) | the workflow-file contract: parseable YAML, runner pinned to `ubuntu-24.04`, TODO(#6224) present, scope limited to kind-e2e.yml, recorded evidence intact | `python3 -I docs/verification/kind-e2e-ubuntu-pin/scripts/verify_contract.py --json` |
| 2. Integration | the workflow *actually executing* on GitHub Actions: re-query the read-only Actions API and assert the PR head's kind-e2e run is green on the `ubuntu-24.04` runner label, plus the recorded failing/control runs | `python3 -I docs/verification/kind-e2e-ubuntu-pin/scripts/verify_ci_evidence.py --json` (needs `gh` auth + network) |
| 3. Live | out of scope by instruction (no live/KUBECONFIG layer); the #6224 follow-up "unload the AppArmor profile on an ubuntu-26.04 runner" is the live experiment that belongs to the tracking issue, not this PR | — |

For a CI-config-only PR there is no Go code to unit-test; layer 1 is the file-contract
test and layer 2 is the real integration surface (GitHub Actions itself). Both emit
go-test-style JSON so the stock `scripts/re-verify.sh` parses them.

> Test polarity: **all tests here are contract tests** (assert the intended state). They
> are RED on the merge base and GREEN on the PR head. There are no bug-canaries: nothing
> in this PR was found to be wrong.

## Problem premise (P0)

Run against the **base** branch without the patch, because the question is whether the
problem exists today. It does.

| | |
|---|---|
| Claimed symptom | "all 5 `kind-e2e-test` matrix jobs fail (… `fusermount3: mount failed: Permission denied`)" and "the job then hangs until the 30-minute step timeout" (issue #6224 / PR body) |
| Linked issue | #6224 "kind-e2e fails on ubuntu-26.04 runners: JindoRuntime FUSE mount denied by host AppArmor fusermount3 profile" — OPEN, created 2026-10-29, labeled `bug` + `Jindo`. The PR explicitly does **not** close it ("This is a temporary mitigation for #6224, which stays open to track the real fix"). |
| Reported component | `.github/workflows/kind-e2e.yml` (E2E Functionality Check) as executed on `ubuntu-latest` → ubuntu-26.04 runners; mechanism sits in the jindo-fuse image + the host AppArmor `fusermount3` profile |
| Patched component | `.github/workflows/kind-e2e.yml` — exact match (the PR pins the runner of exactly the failing job) |
| Component match | **Yes** |
| **Verdict** | **Confirmed** (real problem, reproduced by differential CI evidence) |
| Evidence | `results/ci-evidence.json`, `results/failing-run-40474966878-ubuntu2604.txt` |

Mechanism of the differential (same workflow, same triggering PR #6219, only the runner
image differs):

| Run | Date | `ubuntu-latest` resolved to | kind-e2e (5 jobs) | jindofs-fuse container |
|-----|------|------------------------------|-------------------|------------------------|
| 40125302787 (control) | 2026-10-24 | ubuntu-24.04 (20261012.334.1) | all SUCCESS | `jindoruntime secret e2e SUCCEEDED` |
| 40474966878 (case) | 2026-10-27 | ubuntu-26.04 (20261012.163.1) | all FAILURE at step "Fluid basic e2e tests" | `fusermount3: mount failed: Permission denied` (CrashLoopBackOff) |
| 40885813005 (PR #6225 head) | 2026-10-29 | pinned ubuntu-24.04 (20261025.353.1) | all SUCCESS | all 5 suites SUCCEEDED |

`ubuntu-latest` flipped from the 24.04 to the 26.04 image between 2026-10-24 and
2026-10-27, which is exactly when the breakage started. Backward-compat-test passes on
ubuntu-26.04 (run 40885813116), consistent with the PR leaving that workflow unpinned.

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | kind-e2e is broken on `ubuntu-latest`→26.04 via fusermount3 AppArmor denial | 1+2 | **Confirmed** | failing run log + artifact excerpt; `results/ci-evidence.json` |
| pin-contract | `runs-on: ubuntu-24.04` + TODO(#6224) + parseable workflow on the PR head | 1 | **Confirmed (green)** | `verify_contract.py` — 5/5 pass on head; 2 fail on base |
| scope-guard | only kind-e2e.yml pinned; backward-compat still on `ubuntu-latest` | 1 | **Confirmed (green)** | `TestBackwardCompatWorkflowLeftUnpinned` |
| ci-differential | the PR head's own run is green on the pinned image (all 5 jobs × 5 suites) | 2 | **Confirmed (green)** | `verify_ci_evidence.py` — 3/3 pass (live API) |

## Review findings (ledger)

The review found **no blockers and no majors**. Two advisory findings:

- **F1 [minor, non-blocking]** — Coverage trade-off, deliberate and tracked: pinning
  removes ubuntu-26.04 from the only workflow that exercises the JindoRuntime FUSE path,
  so CI can no longer detect the underlying product-level failure (jindo-fuse +
  `fusermount3` + AppArmor ≥ 5.0 hosts). This is inherent to the mitigation, is recorded
  in open tracking issue #6224 (follow-ups: confirm root cause, fix for 26.04, assess
  production impact, make e2e fail fast), and the PR does not claim to fix it. Not a
  blocker; nothing to merge-block on. `scope-guard` exists so the *remaining* 26.04
  coverage (backward-compat) is not accidentally removed too.
- **F2 [nit]** — The pin has no expiry signal. `ubuntu-24.04` is itself a moving image
  (monthly rolls) and will eventually be retired by GitHub the way 20.04 was; the TODO
  depends on #6224 being driven to done. A dated reminder (e.g. a follow-up checkbox with
  a target quarter) in #6224 would prevent the pin silently outliving its purpose.

Findings refuted during review (not raised): no release branch carries `kind-e2e.yml`
(`origin/release-0.6.0`…`release-1.0` have none), so nothing needs cherry-picking; the
`actions/cache` key (`${{ runner.os }}-kind-…`) is image-agnostic and kind/kubectl are
static binaries, so cache reuse across the image flip is safe; `ubuntu-24.04` is the x64
label, same architecture as `ubuntu-latest`, so no arch drift.

## Harness-bites check

Run on 2026-10-30 (see `verify-manifest.json: harnessBites`): on merge base `fe074af38`,
`TestKindE2eRunnerPinnedToUbuntu2404` and `TestKindE2eTodoReferencesTrackingIssue6224`
FAIL (`runs-on: ubuntu-latest`, no TODO) — the tests genuinely exercise the PR's change.
On PR head `fa06ee36` all 5 unit tests pass. Production code diff is empty (harness-only
branch).

## Re-verify (next round)

```bash
bash docs/verification/kind-e2e-ubuntu-pin/scripts/re-verify.sh            # auto-resolves PR head
bash docs/verification/kind-e2e-ubuntu-pin/scripts/re-verify.sh <fixed-ref> # or explicit ref
```

Exit 0 iff every contract is green. Expected per-finding verdicts while the pin is intact
and its CI run is green: `pin-contract FIXED`, `scope-guard FIXED`, `ci-differential FIXED`.

If a future head switches the runner back (e.g. the #6224 fix lands), `pin-contract` goes
red — that is the harness doing its job; invert/relax it then (the pin's purpose has been
served) rather than treating red as a regression. If `ci-differential` is red because the
PR head's run is still pending, re-run after CI completes.

## Continuing after the fix (possibly on another machine)

1. Graft this harness onto whatever code you want to check:
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/kind-e2e-ubuntu-pin-claude
   git checkout <fixed-ref>
   git checkout FETCH_HEAD -- docs/verification/kind-e2e-ubuntu-pin
   ```
2. Prereqs: layer 1 = python3 + PyYAML; layer 2 = `gh` authenticated with network.
3. Re-run the two commands in the table above, or `scripts/re-verify.sh`.
4. Read results via the polarity note: all contract tests should be GREEN.
5. Harness-bites: run layer 1 once against `fe074af38` and confirm the two pin tests go red.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/kind-e2e-ubuntu-pin-claude of
https://github.com/cheyang/fluid.git (harness for fluid PR #6225). Background: the PR pins
the kind-e2e workflow runner to ubuntu-24.04 to dodge an ubuntu-26.04 AppArmor
fusermount3 denial (tracking issue fluid-cloudnative/fluid#6224). The review confirmed
the premise via CI-run evidence and found no blockers. Read
docs/verification/kind-e2e-ubuntu-pin/README.md ("Continuing after the fix"), graft the
harness onto the current PR head, run scripts/re-verify.sh (layers unit+integration; the
integration layer needs gh auth + network), and report the per-finding verdict table.
If the pin was intentionally reverted because #6224 was fixed, treat pin-contract red as
expected and update the harness rather than reporting a regression. Do not merge, approve,
or comment on the PR.
```
