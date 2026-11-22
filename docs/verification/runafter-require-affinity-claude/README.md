# Verification — runafter-require-affinity — Reviewer A (Claude) — PR #6276

PR: https://github.com/fluid-cloudnative/fluid/pull/6276 (fixes #6275)
Head: `add9a8faa03f4c8aec610efe9b49a4cd79f56792` — Base: `896d21e945e6513ef4dd0e452ea9ec154acb0414`

## Premise (P0) — CONFIRMED

The PR claims `utils.InjectNodeSelectorRequirements` discarded injected requirements when
the affinity already had required node selector terms, because it appended to the *copy*
handed out by `for _, term := range`. Reproduced on **base** with independently written
contract tests: 3 FAIL on base (`results/base-unit-integration.txt`), all PASS on head
(`results/head-unit-integration.txt`). Component match: the issue names exactly the
function and entry point the PR touches. Issue #6275 is OPEN and was filed against the
base commit.

## Harness

All tests are **contract** tests (assert intended behavior; fail on buggy code, pass on
fixed). No canaries. Layers run: unit (`pkg/utils`) + integration (`pkg/dataflow`, fake
API server) — the live layer is skipped per this round's policy.

| Finding | Layer | Test | Base | Head |
| --- | --- | --- | --- | --- |
| F1 requirement appended to every existing term | unit | `TestHarnessInjectAppendsToEveryExistingTerm` | FAIL | PASS |
| F2 requirement injected via `InjectAffinityByRunAfterOp` (engine entry point) | integration | `TestHarnessRequirePolicyWithExistingOwnAffinity` | FAIL | PASS |
| F3 empty-but-non-nil terms get a new term | unit | `TestHarnessInjectEmptyNonNilTermsCreatesTerm` | FAIL | PASS |
| F4 previously-working paths unchanged (nil terms, no-op) | unit | `TestHarnessInjectNilTermsUnchangedBehavior`, `TestHarnessInjectNoRequirementsIsNoop` | PASS | PASS |

Harness-bites check: the same tests are red on base and green on head — the fix is what
flips them (base run doubles as the bite proof).

Full-package regression sweep on head: `go test ./pkg/utils/ ./pkg/dataflow/` —
`results/head-full-suites.txt`, all ok. Caller-package tests exercising the value-file
paths (`pkg/ddc/alluxio`, `pkg/ddc/juicefs`, `pkg/dataprocess`) pass.

## Blast radius checked

- All callers of `InjectNodeSelectorRequirements` → one: `dataflow.injectRequiredAffinity`
  (pkg/dataflow/affinity.go:168), itself called from the DataLoad (alluxio, jindo,
  jindofsx, jindocache, juicefs, cache), DataMigrate (juicefs), DataBackup (alluxio) and
  DataProcess engines — all pass `spec.Affinity` (or nil). In-place mutation now actually
  writes; traced provenance: `utils.GetDataLoad` → `client.Get` into a stack object
  (deepcopy per reconcile), and the spec is never persisted after `Operate` (status
  subresource only). No cache corruption, no duplicate accumulation across reconciles.
- `Prefer` path (`InjectPreferredSchedulingTermsToAffinity`) untouched; webhook
  `InjectNodeSelectorTerms` is a separate, correct cross-product implementation.
- Searched the repo for the same range-copy-append pattern: the three other similar sites
  (`transform_worker.go:192`, `types_selector.go:106`, `require_node_with_fuse.go:93`)
  all use `DeepCopy()` or fresh locals — no other latent copies of this bug.

## Re-verify

```bash
bash docs/verification/runafter-require-affinity-claude/scripts/re-verify.sh
# (no arg: fetches the current PR head from the manifest's pr URL)
```

Expected: all four findings `FIXED`. A finding shows `HARNESS-UPDATE` only if the code
shape changed (renamed symbol / moved file).

## Continuing after the fix

- Graft: `re-verify.sh` checks out the PR head, then restores `harnessPaths` from this
  branch onto it, so it works from any clone of this fork.
- Prereqs: go ≥ 1.22 (repo go.mod), network for `git fetch` of the PR head.
- Polarity table: every test here is a contract test — green means correct, red means
  regression. There are no canaries to invert.
- Kickoff prompt for a fresh agent: "Checkout verify/runafter-require-affinity-claude in
  cheyang/fluid, run docs/verification/runafter-require-affinity-claude/scripts/re-verify.sh
  and report per-finding Fixed/Still-broken/Partial."
