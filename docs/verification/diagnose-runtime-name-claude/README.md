# diagnose-runtime-name — bug verification (PR #6292)

Reproducible evidence for the findings raised while reviewing
https://github.com/fluid-cloudnative/fluid/pull/6292.

Harness branch: `verify/diagnose-runtime-name-claude` (pushed to the reviewer fork
`cheyang/fluid`). **Production code is untouched** — the diff vs the PR head is this
directory only.

| Layer | What it exercises | How to run |
|-------|-------------------|------------|
| 1. Unit (stubbed CLI) | the full `collect` flow of all four `tools/diagnose-fluid-*.sh` against a PATH-stubbed `kubectl`/`helm` that records its argv | `bash scripts/run_harness.sh <repo-dir> <results-dir>` |
| 2. Integration | n/a — no Go/test infra exists for `tools/*.sh`; the stub layer already covers the full script call chain | — |
| 3. Live | skipped per round policy (no cluster) | — |

> Test polarity: **contract** tests only. They FAIL on the buggy (base) code and PASS on
> the fixed (head) code. A red result *is* the reproduction. No bug-canaries here.

## Problem premise (P0)

Run against the **base** branch (`b8ff60e309c341b58a9e26b085b76e6067e6bb60`) without the patch.

| | |
|---|---|
| Claimed symptom | (PR body) "`kubectl_resource` … describes the runtime with `${name}`, a variable these scripts never set … With `${name}` empty, kubectl describes every runtime of that kind in the namespace, and the archive's `<kind>runtime-<name>.yaml` holds all of them instead of the runtime passed with `-r`." |
| Linked issue | none (`#5671` mentioned only; it "also edits these scripts but does not touch this line") |
| Reported component | `tools/diagnose-fluid-{alluxio,curvine,jindo,juicefs}.sh`, `kubectl_resource()` |
| Patched component | exactly those four lines in those four scripts |
| Component match | **Yes** |
| **Verdict** | **Confirmed** |
| Evidence | `results/base-run.txt` — on base the stub receives `kubectl: describe alluxioruntime --namespace ns1` (no resource name), and the archived `alluxioruntime-demo.yaml` is produced by that nameless describe. `git grep -F '${name}'` on base finds exactly the four patched lines and nothing else sets `name`. kubectl's documented behavior for `describe TYPE` with no name is to describe every object of that type in the namespace. |

## Summary of results

| ID | Claim | Layer | Verdict | Evidence |
|----|-------|-------|---------|----------|
| P0 | the reported bug exists on base | 1 | **Confirmed** | `results/base-run.txt` (13 contract failures) |
| F1 | `kubectl describe <kind>runtime` must carry the `-r` runtime name | 1 | **Confirmed, fixed by this PR** | base: FAIL ×4 → head: PASS ×4 (`results/head-run.txt`) |

Observed vs expected (per script, contract form `kubectl describe <kind> --namespace ns1 demo`):

| Script | base (observed) | head (observed) | expected |
|---|---|---|---|
| alluxio | `describe alluxioruntime --namespace ns1` (no name) | `describe alluxioruntime --namespace ns1 demo` | the head form |
| curvine | `describe cacheruntime --namespace ns1` (no name) | `describe cacheruntime --namespace ns1 demo` | the head form |
| jindo | `describe jindoruntime --namespace ns1` (no name) | `describe jindoruntime --namespace ns1 demo` | the head form |
| juicefs | `describe juicefsruntime --namespace ns1` (no name) | `describe juicefsruntime --namespace ns1 demo` | the head form |

## Per-finding detail

- **P0 / F1 (same defect — pre-existing on base, fixed by the PR).** Mechanism: `kubectl_resource()`
  expands the unset `${name}`; unquoted, it word-splits away, so `kubectl describe <kind>runtime -n <ns>`
  runs with no resource name and kubectl describes *every* runtime of that kind in the namespace, while
  the output still lands in `<kind>runtime-<runtime_name>.yaml`. `${runtime_name}` is guaranteed
  non-empty by `runtime_name=${runtime_name:?"…"}` in `collect()`, so the patched form always passes a
  real name. Harness-bites check: with the four scripts reverted to base inside the head worktree the
  harness goes red again (13 failures, `results/reverted-run.txt`), then green after restore — the tests
  exercise the changed path, not an accident.

## Live run notes

Not run (round policy: unit + integration only). The observable live signal, if ever needed: run
`bash tools/diagnose-fluid-alluxio.sh collect -r demo -n ns1` against a real cluster with ≥2
alluxioruntimes in `ns1` and check that `alluxioruntime-demo.yaml` contains only `demo`.

## Proposed fixes

The PR *is* the fix; nothing further proposed for production code here.
(non-blocking, pre-existing nits are recorded in the review findings, not applied:
unquoted `${runtime_name}`/`${runtime_namespace}` expansions throughout the scripts.)

## Continuing after the fix (possibly on another machine)

The harness lives on branch `verify/diagnose-runtime-name-claude`; production code untouched.

1. Graft onto any ref (auto-fetches the current PR head when no ref is given):
   ```bash
   git fetch https://github.com/cheyang/fluid.git verify/diagnose-runtime-name-claude
   git checkout verify/diagnose-runtime-name-claude
   bash docs/verification/diagnose-runtime-name-claude/scripts/re-verify.sh
   ```
2. Prereqs: bash + python3 only (plus the standard `tar`/`git`).
3. Re-run: `bash docs/verification/diagnose-runtime-name-claude/scripts/run_harness.sh <repo-dir>`.
4. Polarity: all tests are contract tests — green means the fix holds. No canaries to invert.
5. Harness-bites: `git checkout <merge-base> -- tools/diagnose-fluid-*.sh && bash …/run_harness.sh .`
   must go red; then `git checkout HEAD -- tools/`.

### Kickoff prompt for a fresh agent

```text
Continue a verification task on branch verify/diagnose-runtime-name-claude
(https://github.com/cheyang/fluid.git). Background: a review of
https://github.com/fluid-cloudnative/fluid/pull/6292 confirmed the premise (P0) and the fix
(F1) with a PATH-stubbed kubectl harness. Read
docs/verification/diagnose-runtime-name-claude/README.md ("Continuing after the fix") and
follow it: run scripts/re-verify.sh against the current PR head, mind the polarity table
(contract tests only), report the per-finding verdicts, and clean up scoped test resources
(/tmp/diagnose_fluid_*). Do not push anything upstream.
```
