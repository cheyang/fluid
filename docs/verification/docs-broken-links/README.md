# Verification: PR #6294 — docs: fix broken relative links

Reviewer: Codex (Reviewer B) · Branch: `verify/docs-broken-links-codex`
Base: `b8ff60e309c341b58a9e26b085b76e6067e6bb60` · Head reviewed: `b164b838fb76c51ca9fc7ee7927185b847a10b43`

## Premise (P0) — verdict: **Confirmed**

Claim (PR body, §I): "Fixes 14 relative links in `docs/` that point at files that don't exist."
Docs-only PR; the premise check and the fix check are the same harness.

- `scripts/check_doc_links.py` resolves every relative link in `docs/**/*.md` against the
  filesystem. On the **base** commit it reports exactly the 14 links the PR claims
  (`results/scan-base.txt`), plus 2 author-disclosed `samples/hdfs` links and 4 scanner
  false positives (regex `Pattern:` cells in generated `docs/{en,zh}/dev/api_doc.md`).
- On the **head** commit all 14 are gone and no new broken link appears
  (`results/scan-head.txt`, `results/contracts-head.txt`).

## Hypotheses / claims

| ID | Claim | Polarity | Layer |
| --- | --- | --- | --- |
| P0-fixes | Each of the 14 fixed links resolves from its source page | contract | unit (script) |
| G1-no-new-broken-links | The PR introduces no new broken relative link | contract | unit (script) |
| F1 | 2 links to `../../../samples/hdfs` (dir removed by #5126) remain broken in `docs/{en,zh}/samples/accelerate_data_accessing_by_hdfs.md` — pre-existing, author-disclosed | canary | unit (script) |

## How to run

```bash
# from a checkout of this branch
bash docs/verification/docs-broken-links/scripts/run_contracts.sh .          # name<TAB>pass|fail
bash docs/verification/docs-broken-links/scripts/re-verify.sh                # auto-fetches current PR head
bash docs/verification/docs-broken-links/scripts/re-verify.sh <sha>          # pin a ref
python3 docs/verification/docs-broken-links/scripts/check_doc_links.py .     # raw scan
```

`re-verify.sh` grafts the harness onto the target ref in a temporary worktree (the
caller's checkout is never modified) and applies polarity: contract tests should be
green when fixed; the canary is fixed only when it flips to fail.

## Observed vs expected

| Run | P0-fixes | G1 | F1 (canary) | exit |
| --- | --- | --- | --- | --- |
| base `b8ff60e` | STILL-BROKEN (fail=14) — reproduces the premise | STILL-BROKEN | STILL-PRESENT (expected) | 1 |
| head `b164b83` | FIXED (pass=14) | FIXED | STILL-PRESENT (expected: PR deliberately left these) | 1 |

Exit code stays 1 on head solely because of the F1 canary; that tracks the one
remaining real defect, not a regression in this PR. Raw output: `results/`.

## Proposed fix for F1 (non-blocking)

`docs/{en,zh}/samples/accelerate_data_accessing_by_hdfs.md` link `../../../samples/hdfs`,
whose target directory was removed by #5126 ("Remove deprecated demo"). Either point the
links at the last tag that carried the demo, or drop the dead references. Left to a
follow-up at the author's explicit choice.

## Continuing after the fix / handoff

1. Fetch this branch; run `re-verify.sh` with no args (resolves the PR head via
   `manifest.pr`, works on any machine). Requirements: git, python3, jq.
2. If F1 is later fixed, the canary flips to fail → invert it or promote the new state
   to a contract test.
3. Advance the marker after each round:
   `git rev-parse <head> > docs/verification/docs-broken-links/.last-reviewed` and commit.

Kickoff prompt for a fresh agent: "Check out `verify/docs-broken-links-codex`, read
`docs/verification/docs-broken-links/README.md`, then run
`bash docs/verification/docs-broken-links/scripts/re-verify.sh`."
