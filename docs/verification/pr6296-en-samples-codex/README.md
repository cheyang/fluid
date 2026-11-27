# Verification harness — PR #6296 (docs: English other_volumes + HDFS configuration samples)

Reviewer: Codex (Reviewer B). Production code untouched — this branch adds only
`docs/verification/pr6296-en-samples-codex/`.

## Premise verdict (P0)

**Confirmed.** The PR claims the zh `other_volumes.md` sample contains
(a) a tab-indented `spec.master.volumeMounts` line that makes
`kubectl create -f dataset.yaml` fail, and (b) `kubectl get secret fluid-secret
-oyaml` split across two lines. Both reproduce on the base ref
(`b8ff60e3`): the YAML heredoc fails parsing with
`found character '\t' that cannot start any token` (line 24 of the manifest),
and the command is split `-oy`/`aml`. Docs-only PR, so premise verification is
formally optional — it was nearly free and is recorded anyway. The
docs-missing-English claim is trivially true (`docs/en/samples/` lacked both
pages on base).

## Findings / claims and how to run

All checks are **contract** tests (they assert intended-correct behavior).
One script runs everything, reading file contents at any git ref via
`git show`, so no checkout juggling is needed:

```bash
# premise control (base): P0a/P0b MUST FAIL; the meta-check asserts exactly that
python3 docs/verification/pr6296-en-samples-codex/scripts/check_docs.py \
  --ref b8ff60e309c341b58a9e26b085b76e6067e6bb60 --expect-premise-fail

# contract suite (head): all 8 checks must PASS
python3 docs/verification/pr6296-en-samples-codex/scripts/check_docs.py --ref <head-sha>

# one-shot re-verification against the current PR head (fetched from manifest.pr)
bash docs/verification/pr6296-en-samples-codex/scripts/re-verify.sh
```

Requires: python3 + PyYAML, jq (re-verify only). No cluster, no Go toolchain.

| ID | claim | polarity | base | head |
|----|-------|----------|------|------|
| P0a | zh dataset.yaml sample parses as valid YAML | contract | FAIL (tab) | PASS |
| P0b | `-oyaml` intact on one line in zh page | contract | FAIL (split) | PASS |
| C1 | en pages' code blocks byte-identical to fixed zh pages | contract | n/a (en missing) | PASS (4/4, 6/6 blocks) |
| C2 | en dataset.yaml parses; volumeMounts/volumes nested under spec | contract | n/a | PASS |
| C3 | relative links in new en pages resolve | contract | n/a | PASS |
| C4 | doc-referenced API fields exist (`hadoopConfig`, `spec.volumes`, `master.volumeMounts`, `/hdfs-config`) | contract | PASS | PASS |

Raw output: `results/base-premise.txt`, `results/head-contracts.txt`,
`results/re-verify-head.txt`.

## Harness-bites check

The contract checks were proven to bite by a throwaway mutation commit
(`secretName: wrong-secret` in the en page + a broken relative link):
C1/C2/C3 flipped to FAIL with the mutated indexes/targets named, then the
mutation branch was deleted. Production diff of this branch is empty.

## Observed vs expected

Everything the PR claims was verified true; no behavioral findings. Two
non-blocking remarks (wording nit in the en hdfs page intro; both new pages
remain absent from `docs/en/TOC.md`, mirroring the pre-existing zh TOC gap)
are in the review document, not in this harness.

## Continuing after a fix / on another machine

```bash
git fetch https://github.com/cheyang/fluid.git verify/pr6296-en-samples-codex
git checkout verify/pr6296-en-samples-codex
bash docs/verification/pr6296-en-samples-codex/scripts/re-verify.sh   # auto-fetches current PR head
```

Kickoff prompt for a fresh agent: "Check out
https://github.com/cheyang/fluid.git branch verify/pr6296-en-samples-codex and
run docs/verification/pr6296-en-samples-codex/scripts/re-verify.sh; report the
per-finding table and the incremental range it prints."
