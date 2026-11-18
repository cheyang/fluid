# Verification — cache-engine-helper-tests (PR #6265)

PR: https://github.com/fluid-cloudnative/fluid/pull/6265
Reviewed head: `c6c73f6b11b8ed5ec788687d1e2372c0a45f32fc` (base `dcb76167b0143a239addc6bed83ed50b45267631`)
Branch: `verify/cache-engine-helper-tests-claude` (based on the PR head; **production code untouched** — the diff is this harness only)

## Premise (P0) — Confirmed

Issue #6263 (OPEN) asks for unit tests of `CheckRuntimeReady`, `GetDataOperationValueFile`,
`findDataOperationSpec`, `validateRuntimeClassTopology` and `Shutdown` in `pkg/ddc/cache/engine`.
On the merge base, `git grep` finds **zero** test references to those symbols in the package
(`validateRuntimeClassTopology` only indirectly via error-path assertions in transform/cm tests).
The PR adds exactly those tests, no production changes. Component match: exact.

## Observed vs expected

| claim | expected | observed | verdict |
| --- | --- | --- | --- |
| P0 premise (untested on base) | symbols untested | zero test refs on base dcb7616 | Confirmed |
| PR-SPECS: 23 new specs pass on head | all pass | 23/23 pass, `Ran 23 of 434 Specs` (results/l1-pr-new-specs.txt) | Pass |
| Full package stays green | no new failures | 434/434 pass with `FLUID_UNIT_TEST=true`; also 434/434 without it on this machine | Pass |
| Tests bite (not vacuous) | regressions detected | 10/10 production mutations CAUGHT (results/l1-mutation-bite.txt) | Pass |
| `go vet` clean | no findings | exit 0 | Pass |

Note on the PR body's "406 passed, 12 failed": on this machine the entire package passes
(434 specs, with and without `FLUID_UNIT_TEST`), so those 12 failures are specific to the
author's environment, not introduced by this PR.

## How to run

```bash
# the PR's own 23 specs (the PR-SPECS contract)
FLUID_UNIT_TEST=true go test -gcflags=all=-l ./pkg/ddc/cache/engine/ \
  -run TestCacheEngine -ginkgo.focus='CacheEngine data operation helpers|validateRuntimeClassTopology|CacheEngine.Shutdown' -count=1

# bite-check: every mutation M1..M10 must be CAUGHT, baselines green
bash docs/verification/cache-engine-helper-tests/scripts/mutation-bite.sh

# re-verify against the current PR head (or a fixed ref)
bash docs/verification/cache-engine-helper-tests/scripts/re-verify.sh
```

`re-verify.sh` auto-resolves the PR head from `verify-manifest.json.pr`, grafts this harness
onto it, and runs the `unit` layer (the PR's 23 focused specs) and the `integration` layer (the
whole package suite). Finding `PR-SPECS` is a **contract** (all listed specs must pass); finding
`PKG-GREEN` additionally requires the full package to stay green. The mutation-bite script
is the manual bite-check recorded in `liveNote`.

Polarity: all findings are contract tests — green means the PR's tests still pass; there is no
canary (no production behavior change to flip).

## Findings summary (review ledger)

No blocker/major findings: the PR is test-only, all specs pass, the package stays green, and the
tests demonstrably detect regressions in every function they cover. Remaining review notes are
nits/questions recorded in the review document (`findings-claude.md` in the run workspace), not
harness claims.

## Continuing after the fix / next round

On any machine with git + go:

```bash
git clone https://github.com/cheyang/fluid.git && cd fluid   # or the upstream clone
git checkout verify/cache-engine-helper-tests-claude
bash docs/verification/cache-engine-helper-tests/scripts/re-verify.sh          # vs current PR head
bash docs/verification/cache-engine-helper-tests/scripts/re-verify.sh <sha>    # vs a specific ref
```

Per-finding output is `Fixed / Still-broken / Partial / Harness-update`. `Harness-update` means
the code shape changed (e.g. specs renamed after an author revision) — update the `match` lists
in `verify-manifest.json`, then re-run. `.last-reviewed` records the sha reviewed this round;
`re-verify.sh` prints the `last-reviewed..head` delta and the one-liner to advance the marker.

Kickoff prompt for a fresh agent:

> You are resuming the review of fluid PR #6265 on branch
> verify/cache-engine-helper-tests-claude. Read
> docs/verification/cache-engine-helper-tests/README.md and verify-manifest.json, run
> scripts/re-verify.sh (no arguments) to check the PR's current head, review the
> last-reviewed..head delta, update the observed-vs-expected table and the ledger, then
> commit and push the branch. Never publish to GitHub without explicit instruction.
