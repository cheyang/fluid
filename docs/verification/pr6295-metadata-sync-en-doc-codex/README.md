# Verification — PR #6295 "docs: add the English metadata sync policy sample"

Reviewer: Codex (Reviewer B). Round: initial. Reviewed head: `038870adb41c7626fd9cc0f3a3cbef4fab989880` (base `b8ff60e309c341b58a9e26b085b76e6067e6bb60`).

## Premise (P0) — CONFIRMED

The PR claims: `docs/zh/samples/metadata_sync.md` (配置元信息同步策略) has no English
version, so English readers cannot find how to turn off
`spec.management.metadataSyncPolicy.autoSync`; the PR adds the translation and
registers it in `docs/en/TOC.md`.

Verified against the **base** branch (`scripts/premise-check.sh`, raw output in
`results/premise-check.txt`):

- `docs/en/samples/metadata_sync.md` does not exist on base.
- On base, the only `docs/en` files mentioning `metadataSyncPolicy`/`autoSync` are
  the generated `dev/api_doc.md` field listing and a JuiceFS-only YAML snippet in
  `samples/juicefs_runtime.md` — neither documents the Alluxio metadata-sync
  policy workflow.

The docs gap is real, and the patched component (docs/en) matches the reported one.

## Hypotheses / acceptance contracts

This PR is docs-only, so the findings from review are the PR's own acceptance
claims, encoded as **contract** tests (PASS on PR head; a future revision that
breaks them goes RED):

| ID | claim | layer | test |
|----|-------|------|------|
| A1 | Page registered in `docs/en/TOC.md` under Dataset → Advanced Configuration, mirroring zh TOC (数据集使用 → 高级配置) at the same position | unit | `TestPR6295EnglishPageRegisteredInTOC` |
| A2 | Fenced code blocks identical to the Chinese source page (modulo trailing whitespace) | unit | `TestPR6295CodeBlocksMatchChineseSource` |
| A3 | Documented field path `spec.management.metadataSyncPolicy.autoSync` and kubectl patch path match the Go JSON tags; default stays sync-on | unit | `TestPR6295MetadataSyncFieldPathMatchesAPI` |
| A4 | Relative links in the new page resolve (`../userguide/install.md`) | unit | `TestPR6295RelativeLinksResolve` |
| A5 | Real docs build (`docs/scripts/mergeByTOC.py`) includes the new page in `en/doc.md` at heading level ### | integration | `TestPR6295DocsBuildIncludesNewPage` |

## How to run

```bash
# L1 unit layer (deterministic, no external deps)
go test ./docs/verification/pr6295-metadata-sync-en-doc-codex/gotest/ \
  -run 'TestPR6295(EnglishPageRegisteredInTOC|CodeBlocksMatchChineseSource|MetadataSyncFieldPathMatchesAPI|RelativeLinksResolve)' -v

# L2 integration layer (needs python3, runs the repo's own docs build in a temp dir)
go test ./docs/verification/pr6295-metadata-sync-en-doc-codex/gotest/ \
  -run 'TestPR6295DocsBuildIncludesNewPage' -v

# L3 live layer: not applicable (docs-only PR; round policy skips KUBECONFIG layer)

# P0 premise (against base)
bash docs/verification/pr6295-metadata-sync-en-doc-codex/scripts/premise-check.sh

# full loop against the current PR head (fetches it from the PR URL)
bash docs/verification/pr6295-metadata-sync-en-doc-codex/scripts/re-verify.sh
```

## Observed vs expected

| ID | expected (contract) | observed on head `038870a` | verdict |
|----|--------------------|-----------------------------|---------|
| P0 | docs gap exists on base | gap confirmed (`results/premise-check.txt`) | Confirmed |
| A1 | TOC registration + zh parity | PASS (`results/head-run.txt`) | Holds |
| A2 | code blocks match zh | 8/8 blocks identical; only delta is one trailing space in zh block 6 dropped by en | Holds |
| A3 | field path matches API | `management`→`metadataSyncPolicy`→`autoSync` JSON tags present; `AutoSyncEnabled()` defaults true | Holds |
| A4 | relative links resolve | 1 relative link (`../userguide/install.md`) resolves | Holds |
| A5 | docs build includes page | `mergeByTOC.py en/` emits `### DEMO - Configure the Metadata Sync Policy` + body into `en/doc.md` | Holds |

Harness-bites check: grafted onto **base** `b8ff60e`, all five tests FAIL
(`results/harness-bites-base.txt`) — the tests exercise the PR's content, not
vacuous truths.

External links (spot check, 2026-11-27): the three `docs.alluxio.io/os/user/stable/en/...`
links return HTTP 200 and match the pattern already used by
`docs/en/samples/accelerate_data_accessing.md`. `mirrors.tuna.tsinghua.edu.cn`
returns 403 to this machine's datacenter IP, but the same URL is used by ~8
existing English docs (repo convention; not introduced by this PR).

## Continuing after a fix / next round

1. Fetch this branch; the harness lives entirely under
   `docs/verification/pr6295-metadata-sync-en-doc-codex/`.
2. `bash docs/verification/pr6295-metadata-sync-en-doc-codex/scripts/re-verify.sh`
   fetches the current PR head (from `manifest.pr`), grafts the harness, runs
   L1+L2 and prints Fixed/Still-broken per contract. Exit 0 iff all hold.
3. All tests are contract polarity — PASS means the property holds. If the PR
   intentionally changes one of these properties (e.g. moves the TOC entry),
   update the contract and note the reason.
4. `.last-reviewed` is the reviewed head; advance it after each round (re-verify
   prints the one-liner).

Kickoff prompt for a fresh agent: "Continue verifying
https://github.com/fluid-cloudnative/fluid/pull/6295 — harness on branch
`verify/pr6295-metadata-sync-en-doc-codex` under
`docs/verification/pr6295-metadata-sync-en-doc-codex/`; run its
`scripts/re-verify.sh` and review the delta from `.last-reviewed`."
