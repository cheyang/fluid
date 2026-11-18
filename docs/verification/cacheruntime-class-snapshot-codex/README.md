# Verification: PR #6242 — bind a CacheRuntime to a snapshot of its CacheRuntimeClass

Reviewer: codex (Reviewer B). Round: initial. Production code is untouched; the branch adds only
the two `verify_pr6242_*_test.go` files and this `docs/verification/` tree.

## Premise (P0) — CONFIRMED

Claim (PR body): "Editing a CacheRuntimeClass can currently change existing CacheRuntimes on their
next reconcile." Linked issue #6213 (open, valid; component matches: `pkg/ddc/cache/engine`).

`TestVerifyPR6242ClassEditDoesNotReachExistingRuntime` is the premise check and the fix contract in
one file; it compiles against both base and head:

| ref | run | observed |
|-----|-----|----------|
| base `956d78a8f` (#6177 head merged with master) | FAIL | class edit changed worker ASTS (rv 999->1000), master ASTS (rv 999->1000) and runtime-config CM (rv 1->2): the premise reproduces |
| head `5ac1064be` | PASS | none of the three change after the same class edit + Sync |

CacheRuntime is verified unreleased (absent from v1.0.8 and `release-1.0` trees), so the PR's
"no migration path" scoping holds.

## Findings verified here

| ID | claim | layer | polarity | result |
|----|-------|-------|----------|--------|
| P0 | class edits propagate to existing runtimes today | unit (Sync end-to-end, fake client) | contract (red on base = premise) | confirmed, see `results/base-p0-premise.txt` |
| C1 | on head, a class edit leaves existing ASTS and runtime config CM untouched | unit | contract | PASS (`results/head-harness.txt`) |
| C2 | a DataLoad whose runtime has no snapshot errors "not bound" (dataset controller retries) and never creates a snapshot | unit | contract | PASS (`results/head-harness.txt`); mutation `getBoundRuntimeClassReadOnly`->`getRuntimeClass` flips it red (`results/harness-bites-mutation.txt`) |

The PR's own 15 snapshot specs (`runtime_class_snapshot_test.go`, incl. stale-owner UID-precondition
replacement, cache-lag wait, malformed snapshot, runtimeClassName warning) were re-run and pass:
`results/head-pr-snapshot-specs.txt` (Ran 15 of 426). Full `./pkg/ddc/cache/... ./pkg/common/...`
suite: `results/head-full-unit.txt`.

## How to run

```bash
# unit layer (everything above)
go test -count=1 -gcflags=all=-l -v ./pkg/ddc/cache/engine/ -run TestVerifyPR6242
go test -count=1 -gcflags=all=-l ./pkg/ddc/cache/... ./pkg/common/...
# premise reproduction on base (expects FAIL):
git checkout 956d78a8f -- pkg/ddc/cache/engine/  # in a scratch worktree, then copy
#   verify_pr6242_class_pinning_test.go into it and run the same -run TestVerifyPR6242ClassEdit test
```

No live/KUBECONFIG layer was run (debate setup: unit + integration only); CI's kind-e2e
(curvine `verify_runtime_class_edit_not_propagated`) is green on the PR.

## Continuing after a fix

This round found no bug to fix; C1/C2 are regression contracts. `scripts/re-verify.sh` (stock
review-finding-verifier script) grafts the harness onto the current PR head (auto-discovered from
`pr` in the manifest) and reports Fixed/Still-broken per finding. `.last-reviewed` pins this
round's head for incremental diffs.

## Round 2 (incremental, 2026-11-18): rebase ff99142f -> 5e647359, head 5ac1064b -> 632ec006

The PR was rebased after #6177 merged; `git range-diff` of the old and new 4-commit series is all
`=` (identical diffs), and the old-head..new-head tree diff on every PR-touched path is empty except
two `curvine_cache_runtime.md` docs files (base-side #6252 heredoc fixes). Base-side commits in the
blast radius: #6177 (merged content identical to what round 1 reviewed), #6244 (adds a capacity-label
refresh inside `lifecycle.SyncScheduleInfoToCacheNodes`, called from `engine/sync.go:125` — additive,
orthogonal to snapshot binding), #6240 (sigs.k8s.io/yaml 1.6.0; the snapshot path uses encoding/json,
dataload only yaml.Marshal-s helm values), #6252 (docs).

Re-verified against the NEW base/head:

| ref | run | observed |
|-----|-----|----------|
| new base `5e647359` | FAIL | premise reproduces identically (worker/master ASTS rv 999->1000, runtime-config CM rv 1->2): `results/base-r2-p0-premise.txt` |
| new head `632ec006` | PASS | C1+C2 via `scripts/re-verify.sh` (`results/reverify/`), full `./pkg/ddc/cache/... ./pkg/common/...` suite green (`results/head-r2-full-unit.txt`), 15/15 snapshot specs green (`results/head-r2-snapshot-specs.txt`) |

Note: the dataload harness file needs PR-only symbols and does not compile on base (expected; on base
there is no snapshot to wait for — the pre-PR live-class fallback IS the premise bug, proven by C1's
base run). CI at this head: staticcheck fails on `pkg/utils/dataset/lifecycle/node_test.go` ST1019
(duplicate import from base-side #6244, untouched by this PR, fixed on master by #6257 1321a217d) —
baseline failure, not a PR regression; all other checks green.
