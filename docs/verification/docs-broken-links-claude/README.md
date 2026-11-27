# Verification — PR #6294 "docs: fix broken relative links" (Reviewer A / Claude)

**Premise (P0): CONFIRMED.** The PR claims "Fixes 14 relative links in `docs/` that
point at files that don't exist". Resolving every relative link in `docs/**/*.md`
against the file system (the model GitHub uses when rendering markdown in a repo:
the target resolves against the containing file's directory) reports **16 broken
links on the merge-base `b8ff60e3`**, of which **14 are in the 8 files this PR
touches** — exactly the 14 edits in the diff. The other 2 are the pre-existing
`samples/hdfs` links the author explicitly documents as out of scope. No
docs-site builder exists in this repo (the website lives in the separate
`fluid-cloudnative/docs-fluid` repo), so GitHub file-relative resolution is the
only in-repo contract to check.

## Harness

Deterministic layer only (a pure file-path-resolution claim needs nothing else):

- `scripts/check_links.py <tree-root>` — walks `docs/**/*.md`, extracts markdown
  links `[text](target)`, skips external (`http(s)`, `mailto`), anchor-only and
  site-absolute (`/…`) targets, skips fenced code blocks and inline code spans
  (GitHub does not render links there), resolves the file-existence part of each
  target relative to the containing file, and prints every miss plus a
  `checked=N broken=M` summary.
- `scripts/re-verify.sh [<ref>]` — fetches the current PR head when no ref is
  given, grafts the checker onto it via `git archive`, and reports finding L1.

## Observed vs expected

| claim | layer | result |
| --- | --- | --- |
| P0: 14 broken relative links exist in the PR-touched files on base | unit | **Confirmed** — 14 lines in `results/base-touched-files-broken.txt`, matching the diff hunk-for-hunk |
| L1 (contract): after the PR, the 8 touched files contain 0 broken relative links | unit | **Pass at head `b164b838f`** — `results/head-links.txt` shows the only remaining broken links in all of `docs/` are the 2 out-of-scope `samples/hdfs` ones (`docs/{en,zh}/samples/accelerate_data_accessing_by_hdfs.md`) |
| Author's side claim: "the same page links it correctly further down" (`preprocess-data.py`) | unit | **Confirmed** — line 236 of `docs/en/samples/vineyard/accelerate_kubeflow_pipelines.md` already used the correct target |
| Author's side claim: zh JuiceFS pages live in a subfolder, en ones do not | unit | **Confirmed** — `docs/zh/samples/juicefs/juicefs_runtime.md` exists; `docs/en/samples/juicefs_runtime.md` exists directly under `docs/en/samples/` (untouched, unbroken on base) |

## Harness bites

`scripts/re-verify.sh` demonstrates red/green directly (the PR head's parent is
the merge-base — single-commit PR — so the base run *is* the fix-removed state):

- `results/reverify-base.out` — L1 **Still-broken, 14 misses**, exit 1
- `results/reverify-head.out` — L1 **Fixed, 0 misses**, exit 0

Full-docs sweep: `results/base-links.txt` (16 broken) vs `results/head-links.txt`
(2 broken, both out of scope). Note: `re-verify.sh` extracts the **full tree**
via `git archive`, not just `docs/` — several correct links in these files point
outside `docs/` (repo-root `static/` images, `samples/` code); an archive of
`docs/` alone would false-positive on them.

## Re-verify after a fix / on another machine

From a checkout of `verify/docs-broken-links-claude`:

```bash
bash docs/verification/docs-broken-links-claude/scripts/re-verify.sh          # current PR head
bash docs/verification/docs-broken-links-claude/scripts/re-verify.sh <sha>    # any ref
```

Exit 0 iff L1 is fixed. All state (manifest, results, `.last-reviewed`) lives on
this branch. No live layer is needed; the claim is decided entirely by the
deterministic layer.

Continuation prompt: "You are resuming review of
https://github.com/fluid-cloudnative/fluid/pull/6294 from branch
verify/docs-broken-links-claude. Run
docs/verification/docs-broken-links-claude/scripts/re-verify.sh (no argument) to
re-check finding L1 against the current PR head, then review the
last-reviewed..head delta per the README."
