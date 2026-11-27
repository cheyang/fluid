# Verification — PR #6295 (docs: add the English metadata sync policy sample)

Reviewer: **Claude (Reviewer A)** · Branch: `verify/docs-metadata-sync-en-claude` ·
PR head reviewed: `038870adb41c7626fd9cc0f3a3cbef4fab989880` ·
Merge-base: `b8ff60e309c341b58a9e26b085b76e6067e6bb60`

## P0 — Premise (runs against BASE)

Claim: *"`docs/zh/samples/metadata_sync.md` has no English version, so English readers can't
find how to turn off `spec.management.metadataSyncPolicy.autoSync`."*

**Verdict: CONFIRMED.** `docs/en/samples/metadata_sync.md` does not exist at the merge-base
and `docs/en/TOC.md` there has no `metadata_sync` entry. See `results/premise-base.txt`.
(Docs-only PR — the premise carve-out would also have applied, but the check is cheap.)

## Observed vs expected

| ID | Claim (contract, unit layer) | Expected | Observed | Verdict |
|----|------------------------------|----------|----------|---------|
| D1 | All fenced code blocks in the en page match the zh source | identical | 8/8 blocks identical (only a trailing space in one zh line differs, ignored) | **PASS** |
| D2 | Relative links in the en page resolve in-repo | all resolve | `../userguide/install.md` exists | **PASS** |
| D3 | `docs/en/TOC.md` lists the page under Advanced Configuration, zh-TOC mirrored | entry + file exist | entry present under `Advanced Configuration`, mirroring zh 高级配置 | **PASS** |
| D4 | Documented path `management.metadataSyncPolicy.autoSync` matches the API | field exists | `RuntimeManagement.MetadataSyncPolicy` → `MetadataSyncPolicy.AutoSync *bool` (json `autoSync`) in `api/v1alpha1/common.go:91-198` | **PASS** |

## Behavioral claims checked by code-path (no cluster needed)

- *"turning off metadata sync ⇒ dataset basic info cannot be computed and shown"*:
  `pkg/ddc/alluxio/metadata.go:85` — `shouldSyncMetadata()` returns false when
  `AutoSyncEnabled()` is false, so `UfsTotal` is never populated. Consistent.
- *"turn automatic metadata sync back on at runtime via kubectl patch"*:
  `pkg/ddc/base/syncs.go:41` — the periodic `TemplateEngine.Sync` calls `SyncMetadata()`,
  and `shouldSyncMetadata()` re-fetches the AlluxioRuntime object on every evaluation,
  so a live patch of `autoSync` is honored on the next sync tick. Consistent.

## External links (manual, not part of re-verify)

All four external URLs return 200 to curl (Alluxio WEB/OSS doc pages,
`downloads.apache.org/hbase/stable/`) except `mirrors.tuna.tsinghua.edu.cn/apache/`
(403 to bots) — the same URL the existing `docs/en/samples/accelerate_data_accessing.md`
already uses, and it appears in the doc only as a WebUFS mountPoint example.

## Harness-bites check

`results/harness-bites.txt` — each of the four checks was run against a deliberately
mutated copy of the doc tree (changed code block, broken link, removed TOC entry,
renamed API field); each mutation made exactly its check fail. The harness discriminates.

## Findings ledger summary

No blocker/major findings. Two nits (see the round ledger in the debate workspace):

- **F1** (nit, pre-existing, carried over verbatim from the zh page): the two sample
  `kubectl get dataset` outputs disagree on CACHE CAPACITY (4.00GiB vs 2.00GiB) while the
  runtime manifest sets `quota: 2Gi`. Differential evidence: D1 parity check proves the en
  page reproduces the zh page byte-for-byte, so this is not introduced by the PR.
- **F2** (nit, optional improvement): the en page could state that `autoSync` defaults to
  `true` (`api/v1alpha1/common.go:195`), which the zh page also omits.

## How to re-run

From a checkout of this branch:

```bash
bash docs/verification/docs-metadata-sync-en/scripts/re-verify.sh          # grafts onto current PR head
bash docs/verification/docs-metadata-sync-en/scripts/re-verify.sh <sha>    # or any ref
python3 docs/verification/docs-metadata-sync-en/scripts/doc_checks.py --repo-root .
```

All four contracts are polarity `contract` (PASS = doc correct). No canaries, so
"all green" == "doc accurate". Requires `python3` and `jq` only.

## Continuing after the fix

If the PR is updated (e.g. the capacity inconsistency is fixed in both en and zh), re-run
`scripts/re-verify.sh` with no argument — it fetches the current PR head from
`manifest.pr`, grafts this harness onto it, and re-runs the unit layer. D1 compares en vs
zh at whatever heads those files are at, so a synchronized fix to both pages keeps D1
green. Then advance `.last-reviewed` to the new head as the script prints.
